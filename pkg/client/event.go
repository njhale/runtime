package client

import (
	"context"

	apiv1 "github.com/acorn-io/acorn/pkg/apis/api.acorn.io/v1"
	"k8s.io/apimachinery/pkg/watch"
	kwatch "k8s.io/apimachinery/pkg/watch"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *DefaultClient) EventList(ctx context.Context) ([]apiv1.Event, error) {
	result := &apiv1.EventList{}
	err := c.Client.List(ctx, result, &kclient.ListOptions{
		Namespace: c.Namespace,
	})
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *DefaultClient) EventStream(ctx context.Context, opts *EventStreamOptions) (<-chan apiv1.Event, error) {
	listOpts := opts.ListOptions()
	listOpts.Namespace = c.Namespace

	current := &apiv1.EventList{}
	if err := c.Client.List(ctx, current, listOpts); err != nil {
		return nil, err
	}

	var (
		w   watch.Interface
		err error
	)
	if opts.Watch {
		w, err = c.Client.Watch(ctx, &apiv1.EventList{}, &kclient.ListOptions{})
	}
	if err != nil {
		return nil, err
	}

	events := make(chan apiv1.Event)
	go func() {
		defer close(events)
		// Send the current set of events
		for _, c := range current.Items {
			events <- c
		}

		if w == nil {
			// Don't need to watch
			return
		}

		defer w.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case e := <-w.ResultChan():
				switch e.Type {
				case kwatch.Error:
					return
				case kwatch.Added, kwatch.Modified:
					// TODO: client-side filtering
					events <- *(e.Object.(*apiv1.Event))
				}
			}
		}
	}()

	return events, nil
}
