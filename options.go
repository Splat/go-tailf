package tailf

import "time"

// Option configures a Tailer.
type Option func(*options)

type options struct {
	fromStart    bool
	pollInterval time.Duration
	notify       <-chan struct{}
	bufSize      int
}

func defaults() options {
	return options{
		pollInterval: 100 * time.Millisecond,
		bufSize:      4096,
	}
}

/*
WithFromStart configures whether to read the file from the beginning.
By default, tailing starts from the end of the file (like tail -f).
*/
func WithFromStart(b bool) Option {
	return func(o *options) {
		o.fromStart = b
	}
}

/*
WithPollInterval sets the interval between EOF poll cycles.
Default is 100ms. Ignored when a notify channel is provided,
but still used as a fallback timeout.
*/
func WithPollInterval(d time.Duration) Option {
	return func(o *options) {
		o.pollInterval = d
	}
}

/*
WithNotify provides an external notification channel that signals
when the file may have new data. This allows integration with
fsnotify or any other file-watching mechanism without adding
a dependency to this library.

When a value is received on the channel, the tailer reads
immediately instead of waiting for the poll interval. The poll
interval is still used as a fallback timeout to handle cases
where notifications are missed.
*/
func WithNotify(ch <-chan struct{}) Option {
	return func(o *options) {
		o.notify = ch
	}
}

/*
WithBufSize sets the initial size of the read buffer in bytes.
Default is 4096.
*/
func WithBufSize(n int) Option {
	return func(o *options) {
		o.bufSize = n
	}
}
