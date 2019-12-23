# rebirth
Supports live reloading for Go

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


# How it Works

`~/work/app` directory is mounted on the container as `/go/src/app`

<img width="600px" src="https://user-images.githubusercontent.com/209884/71261949-f7996500-2381-11ea-9b18-a8e4dfd49c41.png"></img>

1. install `rebirth` CLI ( `go get -u github.com/goccy/rebirth/cmd/rebirth` )
2. run `rebirth` and it cross compile myself for Linux ( GOOS=linux, GOARCH=amd64 ) and put it to `.rebirth` directory as `__rebirth`
3. copy `.rebirth/__rebirth` to the container ( `.rebirth` directory is mounted on the container )
4. watch `main.go` ( by [fsnotify](https://github.com/fsnotify/fsnotify) )

<img width="500px" src="https://user-images.githubusercontent.com/209884/71261979-05e78100-2382-11ea-8955-91e5b01f0234.png"></img>

5. cross compile `main.go` for Linux and put to `.rebirth` directory as `program`
6. copy `.rebirth/program` to the container

<img width="600px" src="https://user-images.githubusercontent.com/209884/71261987-08e27180-2382-11ea-93d3-4117d0dd2999.png"></img>

7. run `__rebirth` on the container
8. `__rebirth` executes `program` 
9. edit `main.go`
10. `rebirth` detects file changed event

<img width="500px" src="https://user-images.githubusercontent.com/209884/71261992-0b44cb80-2382-11ea-9e1d-a2c44f0262ae.png"></img>

11. cross compile `main.go` for Linux and put to `.rebirth` directory as `program`
12. copy `.rebirth/program` to the container
13. `rebirth` send signal to `__rebirth` for reloading ( `SIGHUP` )
14. `__rebirth` kill the current application and execute `program` as a new application

# License

MIT




