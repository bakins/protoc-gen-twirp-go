package example

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	twirp "github.com/twitchtv/twirp"
	"google.golang.org/protobuf/proto"
)

func doTests(t *testing.T, client Haberdasher) {
	resp, err := client.MakeHat(context.Background(), &Size{Inches: 14})
	require.NoError(t, err)
	require.Equal(t, int32(14), resp.Size)

	_, err = client.MakeHat(context.Background(), &Size{Inches: -1})
	require.Error(t, err)
	twerr, ok := err.(twirp.Error)
	require.True(t, ok)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())
}

// TestServer tests new server with original client.
func TestServer(t *testing.T) {
	ts := NewHaberdasherTwirpServer(&testHaberdasher{})
	svr := httptest.NewServer(ts)
	defer svr.Close()

	c := NewHaberdasherProtobufClient(svr.URL, http.DefaultClient)
	doTests(t, c)
}

// TestClient tests new client with original server.
func TestClient(t *testing.T) {
	ts := NewHaberdasherServer(&testHaberdasher{})
	svr := httptest.NewServer(ts)
	defer svr.Close()

	c, err := NewHaberdasherTwirpClient(svr.URL, http.DefaultTransport)
	require.NoError(t, err)

	doTests(t, c)
}

func TestServerPanic(t *testing.T) {
	ts := NewHaberdasherTwirpServer(&panicHaberdasher{})
	svr := httptest.NewServer(ts)
	defer svr.Close()

	c := NewHaberdasherProtobufClient(svr.URL, http.DefaultClient)

	_, err := c.MakeHat(context.Background(), &Size{Inches: -1})
	require.Error(t, err)
	twerr, ok := err.(twirp.Error)
	require.True(t, ok)
	require.Equal(t, twirp.Internal, twerr.Code())
	require.Equal(t, "internal service panic", twerr.Msg())
	require.Equal(t, "very bad things happened", twerr.Meta("cause"))
}

func TestServerContext(t *testing.T) {
	ts := NewHaberdasherTwirpServer(&contextHaberdasher{})
	svr := httptest.NewServer(ts)
	defer svr.Close()

	c := NewHaberdasherProtobufClient(svr.URL, http.DefaultClient)

	_, err := c.MakeHat(context.Background(), &Size{Inches: -1})
	require.Error(t, err)
	twerr, ok := err.(twirp.Error)
	require.True(t, ok)
	require.Equal(t, twirp.DeadlineExceeded, twerr.Code())
	require.Equal(t, "context deadline exceeded", twerr.Msg())
	require.Equal(t, "wrapped error: context deadline exceeded", twerr.Meta("cause"))
}

type contextHaberdasher struct{}

func (h *contextHaberdasher) MakeHat(ctx context.Context, size *Size) (*Hat, error) {
	return nil, fmt.Errorf("wrapped error: %w", context.DeadlineExceeded)
}

type panicHaberdasher struct{}

func (h *panicHaberdasher) MakeHat(ctx context.Context, size *Size) (*Hat, error) {
	panic(errors.New("very bad things happened"))
}

type testHaberdasher struct{}

func (h *testHaberdasher) MakeHat(ctx context.Context, size *Size) (*Hat, error) {
	if size.Inches <= 0 {
		return nil, twirp.InvalidArgumentError("Inches", "I can't make a hat that small!")
	}
	colors := []string{"white", "black", "brown", "red", "blue"}
	names := []string{"bowler", "baseball cap", "top hat", "derby"}
	return &Hat{
		Size: size.Inches,
		// nolint: gosec
		Color: colors[rand.Intn(len(colors))],
		// nolint: gosec
		Name: names[rand.Intn(len(names))],
	}, nil
}

func benchmarkServer(b *testing.B, handler http.Handler) {
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		size := Size{Inches: 14}
		data, err := proto.Marshal(&size)
		if err != nil {
			b.Error(err)
		}

		rdr := bytes.NewReader(data)

		r := httptest.NewRequest(http.MethodPost, "http://localhost/twirp/twitch.twirp.example.Haberdasher/MakeHat", rdr)
		r.Header.Set("Content-Type", "application/protobuf")

		n := noopWriter{
			header: make(http.Header),
		}

		for pb.Next() {
			_, err := rdr.Seek(0, 0)
			if err != nil {
				b.Error(err)
			}

			handler.ServeHTTP(&n, r)

			if n.status != http.StatusOK {
				b.Errorf("unexpected status %d", n.status)
			}
		}
	})
}

func BenchmarkNewServer(b *testing.B) {
	ts := NewHaberdasherTwirpServer(&testHaberdasher{})

	benchmarkServer(b, ts)
}

func BenchmarkOriginalerver(b *testing.B) {
	ts := NewHaberdasherServer(&testHaberdasher{})

	benchmarkServer(b, ts)
}

func BenchmarkNewClient(b *testing.B) {
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		t, err := newTestTransport()
		if err != nil {
			b.Error(err)
		}

		c, err := NewHaberdasherTwirpClient("http://localhost", t)
		if err != nil {
			b.Error(err)
		}

		ctx := context.Background()

		size := Size{
			Inches: 14,
		}

		for pb.Next() {
			_, err := c.MakeHat(ctx, &size)
			if err != nil {
				b.Error(err)
			}
		}
	})
}

func BenchmarkOriginalClient(b *testing.B) {
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		t, err := newTestTransport()
		if err != nil {
			b.Error(err)
		}

		c := NewHaberdasherProtobufClient("http://localhost", &http.Client{Transport: t})

		ctx := context.Background()

		size := Size{
			Inches: 14,
		}

		for pb.Next() {
			_, err := c.MakeHat(ctx, &size)
			if err != nil {
				b.Error(err)
			}
		}
	})
}

type testTransport struct {
	rdr  *bytes.Reader
	resp *http.Response
}

func newTestTransport() (*testTransport, error) {
	hat := Hat{
		Size:  14,
		Color: "red",
		Name:  "bowler",
	}

	data, err := proto.Marshal(&hat)
	if err != nil {
		return nil, err
	}

	rdr := bytes.NewReader(data)

	resp := http.Response{
		Status:     "OK",
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       ioutil.NopCloser(rdr),
	}

	resp.Header.Set("Content-Type", "application/protobuf")

	return &testTransport{
		rdr:  rdr,
		resp: &resp,
	}, nil
}

func (t *testTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	_, err := t.rdr.Seek(0, 0)
	if err != nil {
		return nil, err
	}

	return t.resp, nil
}

type noopWriter struct {
	header http.Header
	status int
}

func (n *noopWriter) Header() http.Header {
	return n.header
}

func (n *noopWriter) Write(b []byte) (int, error) {
	if n.status != http.StatusOK {
		fmt.Println(string(b))
	}
	return len(b), nil
}

func (n *noopWriter) WriteHeader(statusCode int) {
	n.status = statusCode
}
