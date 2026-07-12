-module(echoserver).
-behaviour(gen_server).
-export([init/1, handle_call/3, start/0]).

%% A record for state and a guard on the callback: constructs the Go->Erlang
%% transpiler cannot express, demonstrating the whole-module native escape hatch.
%% Drop-in for the persistent fixture's Go supervisor child spec {echoserver,
%% start, []}; registers {global, echo} so a transpiled-Go client reaches it via
%% otp.CallGlobal("echo", ...). The guard accepts binary|list so it matches
%% whatever the transpiler emits for a Go string argument.
-record(state, {count = 0}).

init(_) -> {ok, #state{}}.

handle_call(Req, _From, #state{count = C} = S) when is_binary(Req); is_list(Req) ->
    {reply, Req, S#state{count = C + 1}}.

start() -> gen_server:start_link({global, echo}, ?MODULE, [], []).
