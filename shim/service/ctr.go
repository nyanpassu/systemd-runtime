package service

import (
	"context"

	any "github.com/golang/protobuf/ptypes/any"
	ptypesEmpty "github.com/golang/protobuf/ptypes/empty"

	"github.com/golang/protobuf/ptypes"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *service) Publish(ctx context.Context, evt *any.Any) (*ptypesEmpty.Empty, error) {
	event, err := ptypes.Empty(evt)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := ptypes.UnmarshalAny(evt, event); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return nil, err
}
