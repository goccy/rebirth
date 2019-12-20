# rebirth
Supports live reloading for Go

# Status

under development

# Features

- Better features than github.com/pilu/fresh
- Supports cross compile and live reloading on host OS for `docker` users ( **Very Fast** for `Docker for Mac` user )
- Supports cross compile by cgo ( C/C++ ) ( currently, works on macOS ( and target architecture is `amd64` ) only )
- Supports helper commands for `go run` `go test` `go build`

# Synopsis

## Settings

`rebirth` needs configuration file ( `rebirth.yml` ) to running .
`rebirth init` create it .

`rebirth.yml` example is the following.

```yaml
host:
  docker: container_name
build:
  env:
    CGO_LDFLAGS: /usr/local/lib/libz.a
run:
  env:
    RUNTIME_ENV: "fuga"
watch:
  root: . # root directory for watching ( default: . )
  ignore:
    - vendor
```

- `host` : specify host information for running to an application ( currently, supports `docker` only )
- `build` : specify ENV variables for building
- `run` : specify ENV variables for running
- `watch` : specify `root` directory or `ignore` directories for watching go file

## In case of running on localhost

### 1. Install `rebirth` CLI

```bash
$ go get -u github.com/goccy/rebirth/cmd/rebirth
```

### 2. Create `rebirth.yml`

```bash
$ rebirth init
```

### 3. Run `rebirth`

```bash
rebirth
```

## In case of running with Docker for Mac

Example tree

```
.
├── docker-compose.yml
├── main.go
└── rebirth.yml
```

`main.go` is your web application's source.

### 1. Install `rebirth` CLI

```bash
$ go get -u github.com/goccy/rebirth/cmd/rebirth
```

### 2. Write settings

### docker-compose.yml

```yaml
version: '2'
services:
  app:
    image: golang:1.13.5
    container_name: rebirth_app
    volumes:
      - '.:/go/src/app'
    working_dir: /go/src/app
    environment:
      GO111MODULE: "on"
    command: |
      tail -f /dev/null
```

### rebirth.yml

```yaml
host:
  docker: rebirth_app # container_name in docker-compose.yml
```

### 3. Run `rebirth`

```bash
$ rebirth

# start live reloading !!

# build for docker container's architecture on macOS (e.g. GOOS=linux GOARCH=amd64
# execute built binary on target container
```

## Helper commands

```bash
Usage:
  rebirth [OPTIONS] <command>

Help Options:
  -h, --help  Show this help message

Available commands:
  build  execute 'go build' command
  init   create rebirth.yml for configuration
  run    execute 'go run'   command
  test   execute 'go test'  command
```

### `rebirth build`

Help cross compile your go script

```bash
$ rebirth build -o app script/hoge.go
```

### `rebirth test`

Help cross compile for `go test`

```bash
$ rebirth test -v ./ -run Hoge
```

### `rebirth run`

Help cross compile for `go run`

```bash
$ rebirth run script/hoge.go
```






