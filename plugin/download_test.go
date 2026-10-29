package plugin

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"google.golang.org/grpc"
)

// downloadStreamFixture 回放下载分块或等待取消
type downloadStreamFixture struct {
	grpc.ClientStream

	ctx    context.Context    // 人工流上下文
	chunks []*wire.MediaChunk // 待回放分块
	wait   bool               // 分块结束后等待上下文取消
	start  chan<- struct{}    // 首次进入等待时通知测试
}

// Recv 回放分块并在需要时等待取消
func (s *downloadStreamFixture) Recv() (*wire.MediaChunk, error) {
	if len(s.chunks) > 0 {
		value := s.chunks[0]
		s.chunks = s.chunks[1:]
		return value, nil
	}
	if s.wait {
		if s.start != nil {
			s.start <- struct{}{}
		}
		<-s.ctx.Done()
		return nil, s.ctx.Err()
	}
	return nil, io.EOF
}

// TestRemoteMediaReaderBoundsAndClose 验证下载流核对长度且 Close 取消底层传输
func TestRemoteMediaReaderBoundsAndClose(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reader := &remoteMediaReader{
		stream: &downloadStreamFixture{ctx: ctx, chunks: []*wire.MediaChunk{{Data: []byte("def")}}},
		cancel: cancel, pending: []byte("abc"), total: 3, expected: 6, limit: 6,
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "abcdef" {
		t.Fatalf("下载内容改变: data=%q err=%v", data, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Close 未取消下载流")
	}
}

// TestRemoteMediaReaderCloseUnblocksRead 验证阻塞读取期间 Close 能解除等待并拒绝后续读取
func TestRemoteMediaReaderCloseUnblocksRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{}, 1)
	reader := &remoteMediaReader{
		ctx: ctx, stream: &downloadStreamFixture{ctx: ctx, wait: true, start: started},
		cancel: cancel, expected: -1, limit: transport.MaxDefaultMediaSize,
	}
	done := make(chan error, 1)
	go func() {
		_, err := reader.Read(make([]byte, 1))
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("下载读取未进入等待")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("Close 未返回关闭错误: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close 后下载仍阻塞")
	}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Close 后仍可读取: %v", err)
	}
}

// TestRemoteMediaReaderCancellation 验证调用取消解除阻塞读取并保留 Canceled
func TestRemoteMediaReaderCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reader := &remoteMediaReader{
		stream: &downloadStreamFixture{ctx: ctx, wait: true},
		cancel: cancel, expected: -1, limit: transport.MaxDefaultMediaSize,
	}
	done := make(chan error, 1)
	go func() {
		_, err := reader.Read(make([]byte, 1))
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		var value *sdk.Error
		if !errors.As(err, &value) || value.Category != sdk.CategoryCanceled {
			t.Fatalf("取消类别丢失: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("取消后下载仍阻塞")
	}
}

// TestRemoteMediaReaderRejectsMalformedChunks 验证空块、超大块和长度不符不会交给插件
func TestRemoteMediaReaderRejectsMalformedChunks(t *testing.T) {
	for _, data := range [][]byte{{}, make([]byte, transport.MaxMediaChunkSize+1)} {
		reader := &remoteMediaReader{cancel: func() {}, expected: -1, limit: transport.MaxDefaultMediaSize}
		if err := reader.validateChunk(data); err == nil {
			t.Fatalf("非法下载分块被接受: %d", len(data))
		}
	}
	reader := &remoteMediaReader{
		stream: &downloadStreamFixture{ctx: t.Context()}, cancel: func() {},
		expected: 2, limit: 2, pending: []byte("x"), total: 1,
	}
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("短下载流被接受")
	}
}
