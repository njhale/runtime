package event

import (
	"context"
	"fmt"

	apiv1 "github.com/acorn-io/acorn/pkg/apis/api.acorn.io/v1"
	"github.com/acorn-io/acorn/pkg/apis/internal.acorn.io/v1"
	"github.com/sirupsen/logrus"
	"k8s.io/apiserver/pkg/endpoints/request"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type Recorder interface {
	Record(context.Context, *apiv1.Event) error
}

type RecorderFunc func(context.Context, *apiv1.Event) error

func (r RecorderFunc) Record(ctx context.Context, e *apiv1.Event) error {
	return r(ctx, e)
}

func NewRecorder(c kclient.Client) RecorderFunc {
	return func(ctx context.Context, e *apiv1.Event) error {
		id, err := ContentID(e)
		if err != nil {
			return fmt.Errorf("failed to generate event name from content: %w", err)
		}
		e.Name = id

		if e.Actor == "" {
			// Set actor from ctx if possible
			logrus.Debug("No Actor set, attempting to set default from ctx")
			if user, ok := request.UserFrom(ctx); ok {
				e.Actor = user.GetName()
			} else {
				logrus.Debug("Ctx has no user info, generating anonymous event")
			}
		}

		return c.Create(ctx, e)
	}
}

func ObjectSource(obj kclient.Object) v1.EventSource {
	return v1.EventSource{
		Kind: obj.GetObjectKind().GroupVersionKind().Kind,
		Name: obj.GetName(),
		UID:  obj.GetUID(),
	}
}
