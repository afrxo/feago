# feago demo — render with `vhs .github/assets/demo.tape`
# Requires: `go build -o ./bin/feago ./cmd/feago` from repo root first.

Output .github/assets/demo.gif

Set Shell "zsh"
Set FontSize 28
Set Width 1600
Set Height 700
Set TypingSpeed 40ms
Set PlaybackSpeed 1.0
Set Padding 26

Env PATH "/Users/davidboakye/dev/go/feago/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"

Hide
Type "cd $(mktemp -d -t feago-demo.XXXXXX)"
Enter
Type "PROMPT='%F{8}❯%f '"
Enter
Type "clear"
Enter
Show

Sleep 400ms
Type "feago init game"
Enter
Sleep 1000ms

Type "cd game"
Enter
Sleep 250ms

Type "ls src/Example"
Enter
Sleep 900ms

Type "feago build"
Enter
Sleep 1100ms

Hide
Type "{ sleep 2 && touch src/Example/Greet.server.luau } >/dev/null 2>&1 &|"
Enter
Sleep 200ms
Type "clear"
Enter
Sleep 150ms
Show

Type "feago watch"
Enter
Sleep 4000ms
