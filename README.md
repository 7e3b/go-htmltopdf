# go-htmltopdf

A concurrent HTML-to-PDF converter for Go, powered by headless Chromium.

The package maintains a pool of Chromium tabs and processes PDF conversions concurrently. Each conversion navigates to a URL and uses Chrome's native `PrintToPDF` functionality.

## Features

* Simple Go API
* Headless Chromium rendering
* Concurrent PDF generation through a configurable worker pool
* Safe for concurrent use
* Context-aware conversion cancellation
* Returns PDF data directly as `[]byte`
* Graceful client shutdown

## Installation

```bash
go get github.com/7e3b/go-htmltopdf
```

## Usage

```go
package main

import (
	"context"
	"log"
	"os"

	htmltopdf "github.com/7e3b/go-htmltopdf"
)

func main() {
	client, err := htmltopdf.New(4)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	url := "https://example.com"

	pdf, err := client.Convert(
		context.Background(),
		&url,
	)
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile("output.pdf", pdf, 0644); err != nil {
		log.Fatal(err)
	}
}
```

## Concurrency

The client maintains a pool of independent Chromium tabs.

```text
                         ┌──────────────┐
                         │    Client    │
                         └──────┬───────┘
                                │
                         ┌──────▼───────┐
                         │    Queue     │
                         └──────┬───────┘
                                │
              ┌─────────────────┼─────────────────┐
              │                 │                 │
        ┌─────▼─────┐     ┌─────▼─────┐     ┌─────▼─────┐
        │  Worker 1 │     │  Worker 2 │     │  Worker 3 │
        │ Chromium  │     │ Chromium  │     │ Chromium  │
        │    Tab    │     │    Tab    │     │    Tab    │
        └───────────┘     └───────────┘     └───────────┘
```

The number passed to `New` controls the number of concurrent workers:

```go
client, err := htmltopdf.New(8)
```

Each worker processes one conversion at a time. If all workers are busy, additional conversions wait in the queue.

A single client can safely be shared between multiple goroutines.

## API

### `New`

```go
func New(workers int) (Client, error)
```

Creates a PDF conversion client with the specified number of workers.

Each worker owns a Chromium tab and processes one conversion at a time.

```go
client, err := htmltopdf.New(4)
if err != nil {
	return err
}
defer client.Close()
```

The worker count should be chosen according to the available CPU and memory resources, since Chromium rendering is resource-intensive.

### `Convert`

```go
func Convert(context.Context, *string) ([]byte, error)
```

Converts the page at the supplied URL to PDF.

```go
url := "https://example.com"

pdf, err := client.Convert(ctx, &url)
if err != nil {
	return err
}
```

The supplied context controls the lifetime of the individual conversion.

For example, a conversion can have a timeout:

```go
ctx, cancel := context.WithTimeout(
	context.Background(),
	30*time.Second,
)
defer cancel()

pdf, err := client.Convert(ctx, &url)
```

If the context is cancelled while the request is waiting or being processed, the conversion is cancelled.

### `Close`

```go
func Close()
```

Stops the client and waits for its workers to exit.

```go
client.Close()
```

`Close` should be called when the client is no longer needed. Using `defer client.Close()` immediately after creating the client is recommended.

## Lifecycle

A client owns a Chromium browser context and a configurable number of worker tabs.

```text
New()
  │
  ├── Chromium browser
  │
  ├── Worker tab
  ├── Worker tab
  ├── Worker tab
  └── ...
  
Convert()
  │
  ▼
 Queue
  │
  ▼
Available worker
  │
  ▼
Navigate to URL
  │
  ▼
Chrome PrintToPDF
  │
  ▼
[]byte
```

When `Close` is called:

1. The client context is cancelled.
2. The queue is closed.
3. Workers stop processing.
4. `Close` waits for all workers to exit.

## Concurrent Conversions

A single client can be shared between goroutines:

```go
client, err := htmltopdf.New(4)
if err != nil {
	log.Fatal(err)
}
defer client.Close()

var wg sync.WaitGroup

for _, url := range urls {
	wg.Add(1)

	go func(url string) {
		defer wg.Done()

		pdf, err := client.Convert(
			context.Background(),
			&url,
		)
		if err != nil {
			log.Printf("convert %s: %v", url, err)
			return
		}

		// Store or process pdf.
		_ = pdf
	}(url)
}

wg.Wait()
```

With four workers, at most four conversions are processed concurrently. Additional conversions wait until a worker becomes available.

## Requirements

`go-htmltopdf` uses [chromedp](https://github.com/chromedp/chromedp) to control a headless Chromium browser.

A compatible Chrome/Chromium installation must therefore be available in the runtime environment.

### `/dev/shm`

Chromium makes significant use of shared memory. When running `go-htmltopdf` inside Docker or another containerized environment, make sure the container has a sufficiently large `/dev/shm`.

The default Docker `/dev/shm` size is often only **64 MB**, which can be insufficient for Chromium, particularly when running multiple workers concurrently.

For example:

```bash
docker run --shm-size=1g your-image
```

Or with Docker Compose:

```yaml
services:
  app:
    image: your-image
    shm_size: 1gb
```

Increase the shared-memory size as the number of Chromium workers and the complexity of the rendered pages increase.

If Chromium becomes unstable, crashes, or pages fail unexpectedly under concurrent load, an undersized `/dev/shm` is one of the things to check.

## Design

The public API intentionally stays small:

```go
type Client interface {
	Convert(context.Context, *string) ([]byte, error)
	Close()
}
```

Internally, conversions are submitted to a concurrent queue and processed by a pool of Chromium tabs.

This separates:

* Request submission
* Queueing
* Concurrency control
* Chromium tab management
* PDF generation

from the application using the package.

## Performance

PDF generation is performed by Chromium, so resource consumption depends on the pages being rendered.

Increasing the worker count increases concurrency, but also increases CPU, memory, and shared-memory usage.

Benchmark different worker counts for your workload rather than assuming that more workers always provide higher throughput.

Factors that can affect conversion performance include:

* HTML complexity
* CSS complexity
* JavaScript execution
* Images and other resources
* Network latency
* PDF size
* Number of concurrent workers
* Available CPU and memory
* Available `/dev/shm`

## Error Handling

Errors are returned from both client initialization and individual conversions.

```go
client, err := htmltopdf.New(4)
if err != nil {
	return err
}
defer client.Close()

pdf, err := client.Convert(ctx, &url)
if err != nil {
	return err
}
```

Errors are wrapped with context to make failures easier to trace.

## License

See [LICENSE](LICENSE).
