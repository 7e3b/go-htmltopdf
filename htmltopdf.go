package htmltopdf

import (
	"context"
	"fmt"
	"sync"

	"github.com/7e3b/go-queue"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Client converts HTML pages to PDF documents using a pool of headless
// Chromium tabs.
//
// A Client is safe for concurrent use. Each conversion is processed by one
// of the configured worker tabs.
//
// Call Close when the Client is no longer needed.
type Client interface {
	// Convert navigates to url and returns the resulting page as a PDF.
	//
	// Convert blocks until the conversion completes, the provided context is
	// cancelled, or the client is closed.
	Convert(context.Context, string) ([]byte, error)

	// Close stops all workers and releases the resources owned by the client.
	//
	// Close waits for in-progress conversions to finish before returning.
	Close()
}

// New creates a Client with workers concurrent Chromium tabs.
//
// Each worker processes one PDF conversion at a time. Increasing workers
// allows multiple conversions to run concurrently, at the cost of additional
// Chromium resources.
//
// New returns an error if the Chromium browser cannot be initialized.
func New(workers int) (Client, error) {
	c, err := newClient(workers)
	if err != nil {
		err = fmt.Errorf("newClient: %w", err)
		return nil, err
	}
	return c, nil
}

type client struct {
	ctx    context.Context
	cancel context.CancelFunc
	queue  queue.Queue[*element]
	wg     *sync.WaitGroup
}

func newClient(workers int) (*client, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &client{
		ctx:    ctx,
		cancel: cancel,
		queue:  queue.New[*element](),
		wg:     &sync.WaitGroup{},
	}
	allocatorCtx, _ := chromedp.NewExecAllocator(
		ctx,
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Headless,
		chromedp.DisableGPU,
	)
	browserCtx, _ := chromedp.NewContext(allocatorCtx)
	err := chromedp.Run(browserCtx)
	if err != nil {
		err = fmt.Errorf("chromedp.Run: %w", err)
		return nil, err
	}
	for range workers {
		c.wg.Add(1)
		tabCtx, _ := chromedp.NewContext(browserCtx)
		tab := &tab{
			wg:    c.wg,
			ctx:   tabCtx,
			queue: c.queue,
		}
		go tab.loop()
	}
	return c, nil
}

type tab struct {
	wg    *sync.WaitGroup
	ctx   context.Context
	queue queue.Queue[*element]
}

func (t *tab) loop() {
	defer t.wg.Done()
	for {
		select {
		case <-t.ctx.Done():
			return
		case element, ok := <-t.queue.Pop():
			if !ok {
				return
			}
			select {
			case <-element.ctx.Done():
				err := fmt.Errorf("element.ctx.Err: %w", element.ctx.Err())
				element.err = err
				close(element.ch)
				continue
			default:
				output, err := t.convert(element)
				if err != nil {
					err = fmt.Errorf("t.convert: %w", err)
					element.err = err
				} else {
					element.output = output
				}
				close(element.ch)
			}
		}
	}
}

func (t *tab) convert(e *element) ([]byte, error) {
	var output []byte
	err := chromedp.Run(
		t.ctx,
		chromedp.Navigate(e.url),
		chromedp.ActionFunc(
			func(ctx context.Context) error {
				params := page.PrintToPDF()
				params = params.WithPrintBackground(true)
				var err error
				output, _, err = params.Do(ctx)
				if err != nil {
					err = fmt.Errorf("params.Do: %w", err)
					return err
				}
				return nil
			},
		),
	)
	if err != nil {
		err = fmt.Errorf("chromedp.Run: %w", err)
		return nil, err
	}
	return output, nil
}

func (c *client) Close() {
	c.cancel()
	c.queue.Close()
	c.wg.Wait()
}

type element struct {
	ctx    context.Context
	url    string
	ch     chan struct{}
	err    error
	output []byte
}

func (c *client) Convert(ctx context.Context, url string) ([]byte, error) {
	e := &element{
		ctx: ctx,
		url: url,
		ch:  make(chan struct{}),
	}
	err := c.queue.Push(e)
	if err != nil {
		err = fmt.Errorf("c.queue.Push: %w", err)
		return nil, err
	}
	select {
	case <-c.ctx.Done():
		err = fmt.Errorf("c.ctx.Err: %w", c.ctx.Err())
		return nil, err
	case <-ctx.Done():
		err = fmt.Errorf("ctx.Err: %w", ctx.Err())
		return nil, err
	case <-e.ch:
		if e.err != nil {
			err = fmt.Errorf("e.err: %w", e.err)
			return nil, err
		}
		return e.output, nil
	}
}
