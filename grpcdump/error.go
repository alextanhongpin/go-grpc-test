package grpcdump

import (
	"google.golang.org/grpc/status"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewError(err error) *Error {
	sts, ok := status.FromError(err)
	if !ok {
		return nil
	}

	return &Error{
		Code:    sts.Code().String(),
		Message: sts.Message(),
	}
}
