-module(echoclient).
-export([main/0]).

main() ->
    global:whereis_name(echo) ! {echo, self(), <<"hello">>},
    receive {ok, Text} -> io:format("~s~n", [Text]) end.
