# Logging

Who may write to standard error, in every language; the Go form is [logging-go](./logging-go.md).

- **A library writes nothing.** Not behind a verbosity flag, not through a global logger a
  command configures: what a library has to say is a returned error or a returned value,
  and a consumer that wants a trace adds it at the call site, where the context is.
- **A command either logs or prints, and knows which.** A tool with diagnostics at levels
  sets up one logger, once, at its entry point, from its flags. A tool whose output to the
  user is messages prints them to standard error as `<cmd>: <message>` and sets the exit
  status, with the quiet levels the tool it mirrors defines; no timestamps, no levels, no
  logging library for three prints.
- **No logger ends the process.** A function returns its error; the entry point prints it
  and exits. A programmer error, an invariant the code itself violated, is a panic, not a
  log line that happens to exit.
