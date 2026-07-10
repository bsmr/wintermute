package transpile

import (
	"os"
	"strings"
	"testing"
)

func TestFile_ModuleAndExport(t *testing.T) {
	src := `package echoserver
func Serve() {}
`
	got, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "-module(echoserver).\n-export([serve/0]).\n\nserve() ->\n    ok.\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFile_ClientBody(t *testing.T) {
	src := `package echoclient
import "go.muehmer.eu/wintermute/pkg/otp"
func Main() {
	otp.Send(otp.Whereis("echo"), "hello")
	otp.Print("done")
}
`
	got, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "-module(echoclient).\n-export([main/0]).\n\n" +
		"main() ->\n" +
		"    whereis(echo) ! <<\"hello\">>,\n" +
		"    io:format(\"~s~n\", [<<\"done\">>]).\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFile_StructLiteralToTuple(t *testing.T) {
	src := `package echoclient
import "go.muehmer.eu/wintermute/pkg/otp"
type Echo struct { From otp.Pid; Text string }
func Main() {
	otp.Send(otp.Whereis("echo"), Echo{From: otp.Self(), Text: "hello"})
}
`
	got, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "whereis(echo) ! {echo, self(), <<\"hello\">>}") {
		t.Fatalf("missing tuple emission:\n%s", got)
	}
}

func TestFile_SpawnNonIdentErrors(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func Boot() { otp.Spawn(makeFn()) }
`
	_, err := File(src)
	if err == nil {
		t.Fatal("want error for non-identifier otp.Spawn argument, got nil")
	}
}

func TestFile_ServerReceiveLoop(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type Echo struct { From otp.Pid; Text string }
type Ok struct { Text string }
func Serve() {
	req := otp.Receive().(Echo)
	otp.Send(req.From, Ok{Text: req.Text})
	Serve()
}
`
	got, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "serve() ->\n" +
		"    receive\n" +
		"        {echo, From, Text} ->\n" +
		"            From ! {ok, Text},\n" +
		"            serve()\n" +
		"    end.\n"
	if !strings.Contains(got, want) {
		t.Fatalf("got:\n%s\nwant contains:\n%s", got, want)
	}
}

func TestFile_GoldenServer(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/echo/go/echoserver/main.go")
	if err != nil {
		t.Fatal(err)
	}
	got, err := File(string(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"-module(echoserver).",
		"receive\n        {echo, From, Text} ->",
		"From ! {ok, Text}",
		"start() -> register(echo, spawn(fun ?MODULE:serve/0)).",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFile_StructLiteralMissingFieldErrors(t *testing.T) {
	src := `package echoclient
import "go.muehmer.eu/wintermute/pkg/otp"
type Echo struct { From otp.Pid; Text string }
func Main() {
	otp.Send(otp.Whereis("echo"), Echo{From: otp.Self()})
}
`
	_, err := File(src)
	if err == nil {
		t.Fatal("want error for struct literal omitting a declared field, got nil")
	}
}

func TestFile_NullaryCallWithArgsErrors(t *testing.T) {
	src := `package m
func Boot() { Helper("x") }
`
	_, err := File(src)
	if err == nil {
		t.Fatal("want error for bare-identifier call with arguments, got nil")
	}
}

func TestFile_FunctionWithParamsErrors(t *testing.T) {
	src := `package m
func Boot(x string) {}
`
	_, err := File(src)
	if err == nil {
		t.Fatal("want error for function with parameters, got nil")
	}
}
