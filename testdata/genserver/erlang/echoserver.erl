-module(echoserver).
-behaviour(gen_server).
-export([init/1, handle_call/3, start/0]).

init(_) -> {ok, {state, 0}}.
handle_call(Req, _From, {state, Count}) -> {reply, Req, {state, Count + 1}}.

start() -> gen_server:start_link({local, echo}, ?MODULE, [], []).
