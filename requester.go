// Package ghz provides gRPC benchmarking functionality
package ghz

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/jhump/protoreflect/dynamic/grpcdynamic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// Options represents the request options
type Options struct {
	Host          string             `json:"host,omitempty"`
	Cert          string             `json:"cert,omitempty"`
	CName         string             `json:"cname,omitempty"`
	N             int                `json:"n,omitempty"`
	C             int                `json:"c,omitempty"`
	QPS           int                `json:"qps,omitempty"`
	Z             time.Duration      `json:"z,omitempty"`
	Timeout       int                `json:"timeout,omitempty"`
	DialTimtout   int                `json:"dialTimeout,omitempty"`
	KeepaliveTime int                `json:"keepAlice,omitempty"`
	Data          interface{}        `json:"data,omitempty"`
	Metadata      *map[string]string `json:"metadata,omitempty"`
	Insecure      bool               `json:"insecure,omitempty"`
}

// Max size of the buffer of result channel.
const maxResult = 1000000

// result of a call
type callResult struct {
	err      error
	status   string
	duration time.Duration
}

// Requester is used for doing the requests
type Requester struct {
	cc       *grpc.ClientConn
	stub     grpcdynamic.Stub
	mtd      *desc.MethodDescriptor
	reporter *Reporter

	data     string
	metadata string

	config  *Options
	results chan *callResult
	stopCh  chan bool
	start   time.Time

	reqCounter int64
}

// New creates new Requester
func New(mtd *desc.MethodDescriptor, c *Options) (*Requester, error) {
	md := mtd.GetInputType()
	payloadMessage := dynamic.NewMessage(md)
	if payloadMessage == nil {
		return nil, fmt.Errorf("No input type of method: %s", mtd.GetName())
	}

	// we need data in string format so
	// we can do template evaluation on it for every call
	dataJSON, err := json.Marshal(c.Data)
	if err != nil {
		return nil, err
	}

	// we need metadata in string format so
	// we can do template evaluation on it for every call
	mdJSON, err := json.Marshal(c.Metadata)
	if err != nil {
		return nil, err
	}

	reqr := &Requester{
		config:   c,
		data:     string(dataJSON),
		metadata: string(mdJSON),
		mtd:      mtd}

	return reqr, nil
}

// Run makes all the requests and returns a report of results
// It blocks until all work is done.
func (b *Requester) Run() (*Report, error) {
	b.results = make(chan *callResult, min(b.config.C*1000, maxResult))
	b.stopCh = make(chan bool, b.config.C)
	b.start = time.Now()

	cc, err := b.connect()
	if err != nil {
		return nil, err
	}

	b.cc = cc
	defer cc.Close()

	b.stub = grpcdynamic.NewStub(cc)

	b.reporter = newReporter(b.results, b.config)

	go func() {
		b.reporter.Run()
	}()

	b.runWorkers()

	report := b.Finish()

	return report, nil
}

// Stop stops the test
func (b *Requester) Stop() {
	// Send stop signal so that workers can stop gracefully.
	for i := 0; i < b.config.C; i++ {
		b.stopCh <- true
	}
}

// Finish finishes the test run
func (b *Requester) Finish() *Report {
	close(b.results)
	total := time.Now().Sub(b.start)

	// Wait until the reporter is done.
	<-b.reporter.done

	return b.reporter.Finalize(total)
}

func (b *Requester) connect() (*grpc.ClientConn, error) {
	var opts []grpc.DialOption
	credOptions, err := createClientCredOption(b.config)
	if err != nil {
		return nil, err
	}

	opts = append(opts, credOptions)

	ctx := context.Background()
	dialTime := time.Duration(b.config.DialTimtout * int(time.Second))
	ctx, _ = context.WithTimeout(ctx, dialTime)
	// cancel is ignored here as connection.Close() is used.
	// See https://godoc.org/google.golang.org/grpc#DialContext

	if b.config.KeepaliveTime > 0 {
		timeout := time.Duration(b.config.KeepaliveTime * int(time.Second))
		opts = append(opts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    timeout,
			Timeout: timeout,
		}))
	}

	opts = append(opts, grpc.WithStatsHandler(&statsHandler{b.results}))

	// create client connection
	return grpc.DialContext(ctx, b.config.Host, opts...)
}

func (b *Requester) runWorkers() {
	var wg sync.WaitGroup
	wg.Add(b.config.C)

	// Ignore the case where b.N % b.C != 0.
	for i := 0; i < b.config.C; i++ {
		go func() {
			defer wg.Done()

			b.runWorker(b.config.N / b.config.C)
		}()
	}
	wg.Wait()
}

func (b *Requester) runWorker(n int) {
	var throttle <-chan time.Time
	if b.config.QPS > 0 {
		throttle = time.Tick(time.Duration(1e6/(b.config.QPS)) * time.Microsecond)
	}

	for i := 0; i < n; i++ {
		// Check if application is stopped. Do not send into a closed channel.
		select {
		case <-b.stopCh:
			return
		default:
			if b.config.QPS > 0 {
				<-throttle
			}

			b.makeRequest()
		}
	}
}

func (b *Requester) makeRequest() {

	reqNum := atomic.AddInt64(&b.reqCounter, 1)

	ctd := newCallTemplateData(b.mtd, reqNum)

	dataMap, err := ctd.executeData(b.data)
	if err != nil {
		return
	}

	mdMap, err := ctd.executeMetadata(b.metadata)
	if err != nil {
		return
	}

	var reqMD *metadata.MD
	if mdMap != nil && len(*mdMap) > 0 {
		md := metadata.New(*mdMap)
		reqMD = &md
	}

	input, streamInput, err := createPayloads(dataMap, b.mtd)
	if err != nil {
		return
	}

	ctx := context.Background()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	timeout := time.Duration(int64(b.config.Timeout) * int64(time.Second))
	ctx, _ = context.WithTimeout(ctx, timeout)

	// include the metadata
	if reqMD != nil {
		ctx = metadata.NewOutgoingContext(ctx, *reqMD)
	}

	if b.mtd.IsClientStreaming() && b.mtd.IsServerStreaming() {
		b.makeBidiRequest(&ctx, streamInput)
	} else if b.mtd.IsClientStreaming() {
		b.makeClientStreamingRequest(&ctx, streamInput)
	} else if b.mtd.IsServerStreaming() {
		b.makeServerStreamingRequest(&ctx, input)
	} else {
		b.stub.InvokeRpc(ctx, b.mtd, input)
	}
}

func (b *Requester) makeClientStreamingRequest(ctx *context.Context, input *[]*dynamic.Message) {
	str, err := b.stub.InvokeRpcClientStream(*ctx, b.mtd)
	counter := 0
	for err == nil {
		streamInput := *input
		inputLen := len(streamInput)
		if input == nil || inputLen == 0 {
			str.CloseAndReceive()
			break
		}

		if counter == inputLen {
			str.CloseAndReceive()
			break
		}

		payload := streamInput[counter]
		err = str.SendMsg(payload)
		if err == io.EOF {
			// We get EOF on send if the server says "go away"
			// We have to use CloseAndReceive to get the actual code
			str.CloseAndReceive()
			break
		}
		counter++
	}
}

func (b *Requester) makeServerStreamingRequest(ctx *context.Context, input *dynamic.Message) {
	str, err := b.stub.InvokeRpcServerStream(*ctx, b.mtd, input)
	for err == nil {
		_, err := str.RecvMsg()
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}
	}
}

func (b *Requester) makeBidiRequest(ctx *context.Context, input *[]*dynamic.Message) {
	str, err := b.stub.InvokeRpcBidiStream(*ctx, b.mtd)
	counter := 0
	for err == nil {
		streamInput := *input
		inputLen := len(streamInput)
		if input == nil || inputLen == 0 {
			str.CloseSend()
			break
		}

		if counter == inputLen {
			str.CloseSend()
			break
		}

		payload := streamInput[counter]
		err = str.SendMsg(payload)
		counter++
	}

	for err == nil {
		_, err := str.RecvMsg()
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}
	}
}

func createClientCredOption(config *Options) (grpc.DialOption, error) {
	if config.Insecure {
		credOptions := grpc.WithInsecure()
		return credOptions, nil
	}

	if strings.TrimSpace(config.Cert) != "" {
		creds, err := credentials.NewClientTLSFromFile(config.Cert, config.CName)
		if err != nil {
			return nil, err
		}
		credOptions := grpc.WithTransportCredentials(creds)
		return credOptions, nil
	}

	credOptions := grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, ""))
	return credOptions, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
