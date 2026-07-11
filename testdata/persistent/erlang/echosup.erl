-module(echosup).
-behaviour(supervisor).
-export([start_link/0, init/1]).

start_link() -> supervisor:start_link({local, echosup}, ?MODULE, []).
init(_) -> {ok, {{one_for_one, 1, 5},
                 [{echo, {echoserver, start, []}, permanent, 5000, worker, [echoserver]}]}}.
