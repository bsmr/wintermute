-module(echoapp).
-behaviour(application).
-export([start/2, stop/1]).

start(_Type, _Args) -> echosup:start_link().
stop(_State) -> ok.
