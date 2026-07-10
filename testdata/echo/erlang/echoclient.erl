-module(echoclient).
-export([main/0]).

main() ->
    whereis(echo) ! {echo, self(), <<"hello">>},
    receive {ok, Text} -> io:format("~s~n", [Text]) end.
