-module(echoserver).
-export([serve/0, start/0]).

serve() ->
    receive
        {echo, From, Text} -> From ! {ok, Text}, serve()
    end.

start() -> register(echo, spawn(fun echoserver:serve/0)).
