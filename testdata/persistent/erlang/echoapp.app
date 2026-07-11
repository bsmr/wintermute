{application, echoapp,
 [{description, "echoapp"},
  {vsn, "0.2.3"},
  {modules, [echoapp, echosup, echoserver]},
  {registered, [echo]},
  {applications, [kernel, stdlib]},
  {mod, {echoapp, []}}]}.