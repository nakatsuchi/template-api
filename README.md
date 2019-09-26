# Template API
A simple REST-API for "text/template" package.

## run
``` shell
export TEMPLATE_BLOB_BUCKET_URL="s3://example?region=us-east-1"
export TEMPLATE_BLOB_PREFIX="templates/"

go run app.go
```

## usage
``` shell 
curl -XPUT http://localhost:8080/templates/example -d "hello {.x}"

curl -XGET http://localhost:8080/templates/example?x=world
# -> hello world
```
