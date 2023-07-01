package grpcdump_test

import (
	"testing"

	"github.com/alextanhongpin/go-grpc-test/grpcdump"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

func TestDump(t *testing.T) {
	md := metadata.New(map[string]string{
		"key": "val",
	})

	d := &grpcdump.Dump{
		FullMethod: "/helloworld.v1.GreeterService/Chat",
		Error: &grpcdump.Error{
			Code:    codes.Unauthenticated.String(),
			Message: "not authenticated",
		},
		Metadata: md,
		Messages: []grpcdump.Message{
			{MessageOrigin: grpcdump.OriginClient, Message: map[string]any{
				"msg": "Hello",
			}},
			{MessageOrigin: grpcdump.OriginServer, Message: map[string]any{
				"msg": "Hi",
			}},
		},
	}
	b, err := d.AsText()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(b))

	d = new(grpcdump.Dump)
	if err := d.FromText(b); err != nil {
		t.Fatal(err)
	}

	t.Logf("%#v", d)
}
