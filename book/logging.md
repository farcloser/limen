# Logging

Who may write to standard error, in every language; the Go form is [logging-go](./logging-go.md).

- **A library logs through the application's logger, never its own output.** A library may
  log, at the levels its records deserve, through the one facility the language's standard
  library gives and the logger the application hands it or installs as the default; the
  application alone decides the handler, the level and the destination. What a library never
  does is choose an output of its own: a print to standard output or error, a private logger
  with its own writer, a format of its own. What it has to say to its caller is still a
  returned error or a returned value; a log record is for the operator, and reaches them only
  as the application decided.
- **A command either logs or prints, and knows which.** A tool with diagnostics at levels
  sets up one logger, once, at its entry point, from its flags. A tool whose output to the
  user is messages prints them to standard error as `<cmd>: <message>` and sets the exit
  status, with the quiet levels the tool it mirrors defines; no timestamps, no levels, no
  logging library for three prints.
- **No logger ends the process.** A function returns its error; the entry point prints it
  and exits. A programmer error, an invariant the code itself violated, is a panic, not a
  log line that happens to exit.
