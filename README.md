# rebirth
Supports live reloading for Go

# Status

under development

# Features

- Supports cross compile and hot reloading on macos for `Docker for Mac` users

# Synopsis

## In case of running Web Apps with Docker for Mac

Example tree

```
.
├── docker-compose.yml
├── main.go
└── rebirth.yml
```

`main.go` is your web application's source.

### docker-compose.yml

```yaml
version: '2'
services:
  app:
    image: golang:1.13.5
    container_name: app
    volumes:
      - '.:/go/src/app'
    working_dir: /go/src/app
    environment:
      GO111MODULE: "on"
    command: |
      tail -f /dev/null
```

And write configuration file for `rebirth`

### rebirth.yml

```yaml
host:
  docker: app # container_name in docker-compose.yml
```

Then, install `rebirth` CLI

```bash
$ go get -u github.com/goccy/rebirth/cmd/rebirth
```

Finnaly, run `rebirth`

```bash
$ rebirth

# start live reloading !!

# build for docker container's architecture on macOS (e.g. GOOS=linux GOARCH=amd64
# execute built binary on target container
```


