package channel

import "context"

type Alerter interface {
	Alert(ctx context.Context, format string, v ...interface{})
}
