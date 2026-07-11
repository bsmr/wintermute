-module(echoclient).
-export([main/0]).

main() -> io:format("~s~n", [gen_server:call({global, echo}, <<"hello">>)]).
