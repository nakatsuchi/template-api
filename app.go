package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/patrickmn/go-cache"
	"gocloud.dev/blob"
	_ "gocloud.dev/blob/azureblob"
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/s3blob"
	"gocloud.dev/gcerrors"
)

var (
	blobBucketURL      = os.Getenv("TEMPLATE_BLOB_BUCKET_URL")
	blobPrefix         = os.Getenv("TEMPLATE_BLOB_PREFIX")
	templateCache      = cache.New(5*time.Minute, 10*time.Minute)
	templateCacheMutex = new(sync.Mutex)
)

func main() {
	r := gin.Default()

	r.PUT("/templates/:templatename", func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			c.AbortWithError(400, err)
		}

		templateName := c.Param("templatename")

		bodyStr := string(body)
		// validation
		_, err = template.New(templateName).Parse(bodyStr)
		if err != nil {
			c.AbortWithError(400, err)
			return
		}

		err = saveTemplate(c, templateName, bodyStr)
		if err != nil {
			c.AbortWithError(500, err)
			return
		}
	})

	r.GET("/templates/:templatename", func(c *gin.Context) {
		templateName := c.Param("templatename")

		tmpl, err := getTemplateWithCaching(c, templateName)
		if err != nil {
			c.AbortWithError(400, err)
			return
		}
		if tmpl == nil {
			c.AbortWithStatus(404)
			return
		}

		c.String(200, tmpl.Root.String())
	})

	r.DELETE("/templates/:templatename", func(c *gin.Context) {
		templateName := c.Param("templatename")

		err := deleteTemplate(c, templateName)
		if err != nil {
			c.AbortWithError(400, err)
			return
		}

		c.Status(204)
	})

	r.GET("/templates/:templatename/apply", func(c *gin.Context) {
		templateName := c.Param("templatename")
		params := map[string]interface{}{}
		for k, v := range c.Request.URL.Query() {
			if strings.HasSuffix(k, "[]") {
				// treat as array
				params[strings.TrimSuffix(k, "[]")] = v
				continue
			}
			params[k] = v[0]
		}

		tmpl, err := getTemplateWithCaching(c, templateName)
		if err != nil {
			c.AbortWithError(500, err)
			return
		}
		if tmpl == nil {
			c.AbortWithStatus(404)
			return
		}
		fmt.Println(params)

		buf := bytes.Buffer{}
		err = tmpl.Execute(&buf, params)
		if err != nil {
			c.AbortWithError(400, err)
			return
		}

		c.String(200, buf.String())
	})

	r.Run()
}

func openPrefixedBucket(c context.Context) (*blob.Bucket, error) {
	bucket, err := blob.OpenBucket(c, blobBucketURL)
	if err != nil {
		return nil, err
	}

	return blob.PrefixedBucket(bucket, blobPrefix), nil
}

func saveTemplate(ctx context.Context, name string, text string) error {
	bucket, err := openPrefixedBucket(ctx)
	if err != nil {
		return err
	}

	blobWriter, err := bucket.NewWriter(ctx, name, nil)
	if err != nil {
		return err
	}

	_, err = blobWriter.Write([]byte(text))
	if err != nil {
		blobWriter.Close()
		return err
	}

	err = blobWriter.Close()
	if err != nil {
		return err
	}

	templateCache.Delete(name)
	return nil
}

func getTemplateWithCaching(ctx context.Context, name string) (*template.Template, error) {
	tmpl, found := templateCache.Get(name)
	if found {
		return tmpl.(*template.Template), nil
	}

	templateCacheMutex.Lock()
	defer templateCacheMutex.Unlock()

	tmpl, found = templateCache.Get(name)
	if found {
		return tmpl.(*template.Template), nil
	}

	tmpl, err := getTemplate(ctx, name)
	if err != nil {
		return nil, err
	}
	templateCache.SetDefault(name, tmpl)
	return tmpl.(*template.Template), nil
}

func getTemplate(ctx context.Context, name string) (*template.Template, error) {
	bucket, err := openPrefixedBucket(ctx)
	if err != nil {
		return nil, err
	}

	blobReader, err := bucket.NewReader(ctx, name, nil)
	if gcerrors.Code(err) == gcerrors.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	defer blobReader.Close()
	data, err := ioutil.ReadAll(blobReader)
	if err != nil {
		return nil, err
	}

	return template.New(name).Parse(string(data))
}

func deleteTemplate(ctx context.Context, name string) error {
	bucket, err := openPrefixedBucket(ctx)
	if err != nil {
		return err
	}

	bucket.Delete(ctx, name)
	templateCache.Delete(name)
	return err
}
