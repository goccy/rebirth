package errors

import "golang.org/x/xerrors"

var (
	ErrCrossCompiler = xerrors.New(`
Please install cross compiler by the following command

$ brew install FiloSottile/musl-cross/musl-cross

( Sorry, wait about 30 minutes... )
`)
)
