package htmltopdf

import (
	"context"
	"sync"
)

type queue struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     *sync.Mutex
	store  []string
	sigCh  chan struct{}
	subCh  chan string
	wg     *sync.WaitGroup
}

func newQueue() *queue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &queue{
		ctx:    ctx,
		cancel: cancel,
		mu:     &sync.Mutex{},
		store:  []string{},
		sigCh:  make(chan struct{}, 1),
		subCh:  make(chan string),
		wg:     &sync.WaitGroup{},
	}
	q.wg.Add(1)
	go q.loop()
	return q
}

func (q *queue) push(input string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	err := q.ctx.Err()
	if err != nil {
		return err
	}
	q.store = append(q.store, input)
	select {
	case q.sigCh <- struct{}{}:
	default:
	}
	return nil
}

func (q *queue) loop() {
	defer q.wg.Done()
	for {
		select {
		case <-q.ctx.Done():
			return
		case <-q.sigCh:
			q.mu.Lock()
			if len(q.store) == 0 {
				q.mu.Unlock()
				continue
			}
			values := q.store
			q.store = []string{}
			q.mu.Unlock()
			for _, value := range values {
				select {
				case <-q.ctx.Done():
					return
				case q.subCh <- value:
				}
			}
		}
	}
}

func (q *queue) pop() (string, error) {
	select {
	case <-q.ctx.Done():
		return "", q.ctx.Err()
	case value := <-q.subCh:
		return value, nil
	}
}

func (q *queue) close() {
	q.mu.Lock()
	q.cancel()
	q.mu.Unlock()
	q.wg.Wait()
}

