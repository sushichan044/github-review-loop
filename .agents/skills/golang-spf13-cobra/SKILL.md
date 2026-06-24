---
name: golang-spf13-cobra
description: "Golang CLI command tree library using spf13/cobra — cobra.Command, RunE vs Run, PersistentPreRunE hook chain, Args validators (NoArgs, ExactArgs, MatchAll, custom), persistent vs local flags, command groups, ValidArgsFunction, RegisterFlagCompletionFunc, ShellCompDirective, usage/help template customization, man-page and markdown doc generation, and testing with SetArgs/SetOut/SetErr. Apply when using or adopting spf13/cobra, or when the codebase imports `github.com/spf13/cobra`. For configuration layering alongside cobra, see the `samber/cc-skills-golang@golang-spf13-viper` skill. For general CLI architecture (project layout, exit codes, signal handling, I/O patterns), see `samber/cc-skills-golang@golang-cli`."
user-invocable: true
license: MIT
compatibility: Designed for Claude Code or similar AI coding agents, and for projects using Golang.
metadata:
  author: samber
  version: "1.0.2"
  openclaw:
    emoji: "🐍"
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
        - go
    install: []
    skill-library-version: "1.10.2"
allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent WebFetch mcp__context7__resolve-library-id mcp__context7__query-docs
---

**Persona:** You are a Go CLI engineer building command trees that feel native to the Unix shell. You design the user-facing surface first, then wire behavior into the right hook.

**Modes:**

- **Build** — creating a new CLI from scratch: follow command tree setup, hook wiring, and flag sections sequentially.
- **Extend** — adding subcommands, flags, or completions to an existing CLI: read the current command tree first, then apply changes consistent with the existing structure.
- **Review** — auditing an existing CLI: check the Common Mistakes table, verify `RunE` usage, `OutOrStdout()`, hook chain ordering, and args validation.

# Using spf13/cobra for CLI command trees in Go

Cobra is the de facto standard for Go CLI applications. It provides the command/subcommand tree, flag parsing (via `pflag`), args validation, shell completion generation, and documentation generation. It does **not** handle configuration layering — that's viper's job.

**Official Resources:**

- [pkg.go.dev/github.com/spf13/cobra](https://pkg.go.dev/github.com/spf13/cobra)
- [github.com/spf13/cobra](https://github.com/spf13/cobra)
- [cobra.dev](https://cobra.dev)

This skill is not exhaustive. Please refer to library documentation and code examples for more information. Context7 can help as a discoverability platform. For Go package docs, versions, symbols, and known vulnerabilities, → See `samber/cc-skills-golang@golang-pkg-go-dev` skill.

```bash
go get github.com/spf13/cobra@latest
```

## Cobra vs. viper

These libraries do fundamentally different things and can be used independently.

| Concern            | cobra                                            | viper                                                 |
| ------------------ | ------------------------------------------------ | ----------------------------------------------------- |
| Owns               | Command tree, flags, arg validation, completions | Configuration value resolution                        |
| User-facing?       | Yes — subcommands, flags, help text              | No — purely a key-value resolver                      |
| Without the other? | Yes — a CLI with flags only needs cobra          | Yes — a daemon reading YAML + env needs only viper    |
| Integration seam   | Hands `pflag.Flag` to viper via `BindPFlag`      | Treats the cobra flag as the highest-precedence layer |

**Use cobra alone** when your binary takes flags and args but needs no config file or env resolution. **Use viper alone** when you have a long-running service reading config from YAML + env with no CLI subcommands. Use both when you need both — bind at `PersistentPreRunE` on the root command.

→ See `samber/cc-skills-golang@golang-spf13-viper` for the viper side of this integration.

## Command tree

Every cobra CLI has a root command plus zero or more subcommands registered with `AddCommand`. The root command name is the binary name.

```go
var rootCmd = &cobra.Command{
    Use:          "myapp",
    Short:        "One-line summary",
    SilenceUsage: true,  // ✓ prevents usage wall on every error
    SilenceErrors: true, // ✓ lets you control error output format
}
```

Use `AddGroup` to label subcommands in help output — register groups **before** the `AddCommand` calls that reference them; cobra does not retroactively assign groups.

## The Run\* family

Cobra commands have five run hooks executed in order:

```
PersistentPreRunE → PreRunE → RunE → PostRunE → PersistentPostRunE
```

Always use `*E` variants — the non-`E` forms cannot return errors. Key rules:

- `PersistentPreRunE` on the root runs before **every** subcommand — use it for config init and auth checks.
- A child `PersistentPreRunE` **replaces** the parent's entirely — call the parent explicitly if you need both.
- `PostRunE` runs only if `RunE` succeeded.

For the full lifecycle and inheritance rules, see [commands-and-args.md](references/commands-and-args.md).

## Args validators

Cobra validates positional arguments before `RunE` runs. Never write `len(args)` checks inside `RunE` — that bypasses cobra's standard error messages and arg count tracking.

Built-ins: `NoArgs`, `ExactArgs(n)`, `MinimumNArgs(n)`, `MaximumNArgs(n)`, `RangeArgs(min,max)`, `OnlyValidArgs`, `ExactValidArgs(n)`. Compose with `MatchAll(v1, v2)`. Custom validator: `func(cmd *cobra.Command, args []string) error`.

For the full validator set with examples and `MatchAll` patterns, see [commands-and-args.md](references/commands-and-args.md).

## Flags primer

Cobra delegates flag parsing to `pflag`. **Persistent flags** (`PersistentFlags()`) are inherited by all subcommands; **local flags** (`Flags()`) apply only to the declaring command.

```go
rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path") // inherited by all subcommands
serveCmd.Flags().IntVar(&port, "port", 8080, "listen port")                     // local to serveCmd only
serveCmd.MarkFlagRequired("port")
serveCmd.MarkFlagsMutuallyExclusive("json", "yaml")
```

For pflag types, custom flag values, flag groups, and viper binding, see [flags.md](references/flags.md).

## Completions primer

Cobra generates shell completions automatically. Extend them with:

- **`ValidArgs []string`** — static positional arg completion.
- **`ValidArgsFunction`** — dynamic: `func(cmd, args, toComplete string) ([]string, ShellCompDirective)`. Return `ShellCompDirectiveNoFileComp` to suppress file fallback.
- **`RegisterFlagCompletionFunc(name, fn)`** — flag value completion.

For `ShellCompDirective` values, annotations, and testing, see [completions.md](references/completions.md).

## Testing commands

Test commands by executing them programmatically. **Never use `os.Stdout` / `os.Stderr` directly** in command handlers — use `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` so tests can redirect output.

```go
func TestServeCmd(t *testing.T) {
    buf := new(bytes.Buffer)
    rootCmd.SetOut(buf)
    rootCmd.SetArgs([]string{"serve", "--port", "9090"})
    require.NoError(t, rootCmd.Execute())
    assert.Contains(t, buf.String(), "listening on :9090")
}
```

Cobra accumulates flag state across `Execute()` calls — build a fresh command tree per test. For isolation patterns, golden files, and testing completions, see [testing.md](references/testing.md).

## Best Practices

1. **Always use `RunE`, never `Run`** — `Run` cannot return an error; the only escape is `os.Exit` or panic, bypassing defers.
2. **Put config initialization in `PersistentPreRunE`** — it runs before every subcommand; the right place for viper binding and auth checks.
3. **Validate positional args with `Args`, not inside `RunE`** — `Args` gives cobra's standard error messages; `MatchAll` composes validators.
4. **Use `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` for all output** — direct `os.Stdout` writes cannot be captured by tests.
5. **Re-create the command tree per test** — cobra accumulates flag state across `Execute()` calls on the same instance.

## Common Mistakes

| Mistake                                           | Why it fails                                                                 | Fix                                                              |
| ------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| Using `Run` instead of `RunE`                     | Cannot return an error — only escape is `os.Exit` or panic, bypassing defers | Use `RunE` — return the error, let cobra handle the exit         |
| Writing `len(args)` checks in `RunE`              | Bypasses cobra's standard error messages ("accepts 1 arg, received 2")       | Declare `Args: cobra.ExactArgs(1)` on the command                |
| Writing to `os.Stdout` directly                   | Tests cannot capture output — os-level file handles can't be redirected      | Use `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`                    |
| Child `PersistentPreRunE` silently drops parent's | Cobra does not chain — the child replaces the parent's hook entirely         | Call `parent.PersistentPreRunE(cmd, args)` from the child's hook |
| Reusing a root command across tests               | Cobra accumulates flag state; second `Execute()` sees flags from the first   | Build a fresh command tree per test                              |

## Further Reading

- [commands-and-args.md](references/commands-and-args.md) — full PreRun\*/PostRun\* chain, every Args validator, PersistentPreRunE inheritance rules
- [flags.md](references/flags.md) — pflag types, required/exclusive/oneRequired groups, custom value types, viper binding
- [completions.md](references/completions.md) — ShellCompDirective set, annotation-based completions, testing completions
- [generators.md](references/generators.md) — man page, markdown, YAML, RST doc generation; `cobra-cli` scaffolder
- [testing.md](references/testing.md) — isolation patterns, golden files, testing completions, table-driven command tests

## Cross-References

- → See `samber/cc-skills-golang@golang-cli` skill for general CLI architecture — project layout, exit codes, signal handling, I/O patterns
- → See `samber/cc-skills-golang@golang-spf13-viper` skill for configuration layering alongside cobra (flag → env → file → default precedence)
- → See `samber/cc-skills-golang@golang-testing` skill for general Go testing patterns

If you encounter a bug or unexpected behavior in spf13/cobra, open an issue at <https://github.com/spf13/cobra/issues>.
