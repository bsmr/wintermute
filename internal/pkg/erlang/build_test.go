package erlang

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildAssemblesConfigureMakeInstall(t *testing.T) {
	var cmds []string
	b := Builder{
		Home: "/home/u",
		Out:  io.Discard,
		Run: func(_ context.Context, dir, name string, args ...string) error {
			cmd := dir + "|" + name
			if len(args) > 0 {
				cmd += " " + strings.Join(args, " ")
			}
			cmds = append(cmds, cmd)
			return nil
		},
	}
	if err := b.Build(context.Background(), "29.0.3"); err != nil {
		t.Fatal(err)
	}
	src := "/home/u/.local/erlang/29.0.3/src"
	want := []string{
		src + "|./configure --prefix=/home/u/.local/erlang/29.0.3",
		src + "|make",
		src + "|make install",
	}
	if len(cmds) != len(want) {
		t.Fatalf("commands = %v", cmds)
	}
	for i := range want {
		if cmds[i] != want[i] {
			t.Fatalf("cmd[%d] = %q, want %q", i, cmds[i], want[i])
		}
	}
}

func TestFetchSourceVerifiesSHA(t *testing.T) {
	body := []byte("fake tarball bytes")
	sum := sha256.Sum256(body)
	good := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	got, err := fetchSource(context.Background(), srv.URL, good)
	if err != nil {
		t.Fatalf("matching sha should pass: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("body = %q", got)
	}
	if _, err := fetchSource(context.Background(), srv.URL, "deadbeef"); err == nil {
		t.Fatal("mismatched sha should error, got nil")
	}
}

func TestFetchSourceSizeCeiling(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write(make([]byte, (200<<20)+1)) // one byte over the ceiling
	}))
	defer srv.Close()
	_, err := fetchSource(context.Background(), srv.URL, "irrelevant")
	if err == nil || !strings.Contains(err.Error(), "ceiling") {
		t.Fatalf("want size-ceiling error, got: %v", err)
	}
	if calls != 1 {
		t.Fatalf("size-cap error must not be retried, calls = %d", calls)
	}
}

func TestFetchSourceRetries(t *testing.T) {
	body := []byte("ok")
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	got, err := fetchSource(context.Background(), srv.URL, want)
	if err != nil || string(got) != "ok" {
		t.Fatalf("retry should succeed on 2nd attempt: got %q err %v", got, err)
	}
	if calls < 2 {
		t.Fatalf("expected a retry, calls = %d", calls)
	}
}

func TestTarSupportsStrip(t *testing.T) {
	yes := []string{"tar (GNU tar) 1.35", "bsdtar 3.5.3 - libarchive 3.5.3"}
	no := []string{"BusyBox v1.36.1", "tar: unknown", ""}
	for _, v := range yes {
		if !tarSupportsStrip(v) {
			t.Fatalf("tarSupportsStrip(%q) = false, want true", v)
		}
	}
	for _, v := range no {
		if tarSupportsStrip(v) {
			t.Fatalf("tarSupportsStrip(%q) = true, want false", v)
		}
	}
}
