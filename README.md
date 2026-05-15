# feago

![GitHub License](https://img.shields.io/github/license/afrxo/feago?style=for-the-badge)
![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/afrxo/feago/ci.yml?style=for-the-badge)
![Latest Release](https://img.shields.io/github/v/release/afrxo/feago?style=for-the-badge)

**feago** is a Rojo project generator for people who want to organize their Roblox code by feature instead of by realm.

You write code in folders like `src/combat/`, `src/inventory/`, and feago decides which files belong on the server, client, or in shared, then writes the Rojo project file for you.

## Why

Realm-first layouts (`src/server/`, `src/client/`, `src/shared/`) scatter a single feature across three places. Touching one feature means jumping between three folders. feago flips that. One feature, one folder, mixed code.

## Features

* **Feature-based source layout.** Server, client, and shared code live next to each other.
* **Per-file realm resolution.** You can use a filename suffix, a directive on the first line, or a `.feago` file in the folder.
* **Live rebuild.** `feago watch` watches `src/` and rebuilds the project file when you edit, add, or delete a file.
* **Standard Rojo output.** Output is a normal Rojo project file.

## Install

**Rokit (recommended, per-project):**

```sh
rokit add afrxo/feago
```

**Homebrew (macOS, global):**

```sh
brew install --cask afrxo/tap/feago
```

**Prebuilt binary:** grab one from the [releases page](https://github.com/afrxo/feago/releases).

## Quickstart

```sh
feago init my-game
cd my-game
feago watch
```

Resulting layout:

```
my-game/
  default.project.json
  src/
    combat/
      combat.server.luau
      combat.client.luau
      combat-shared.luau
    inventory/
      .feago
      slots.luau
      ui.client.luau
```

## How realm gets decided

For each `.luau` file, feago checks in this order and stops at the first match:

1. Filename suffix, same as Rojo (`*.server.luau`, `*.client.luau`).
2. First-line directive. `--@load:server`, `--@load:client`, `--@load:shared`, or `--@load:preload`.
3. Closest `.feago` folder config walking up the folder tree (see below).
4. Default: shared.

> [!IMPORTANT]
> A directive beats the filename suffix. `*.client.luau` with `--@load:preload` on the first line maps to preload (`ReplicatedFirst`), not client.

Realms map to services like this:

| Realm   | Rojo destination                |
|---------|---------------------------------|
| Server  | `ServerScriptService`           |
| Client  | `ReplicatedStorage/Client`      |
| Shared  | `ReplicatedStorage/Shared`      |
| Preload | `ReplicatedFirst`               |

> [!NOTE]
> These destinations are hardcoded for now. Making them configurable per project is planned.

### The `.feago` folder config

A `.feago` file is a config you drop into a folder to set the default realm for every `.luau` file under it (unless a file overrides it with a suffix or a directive).

```ini
# src/inventory/.feago
realm = shared
```

Valid values: `server`, `client`, `shared`, `preload`. Lines starting with `#` are comments.

> [!CAUTION]
> A folder default loses to per-file overrides. A stray `combat.server.luau` inside a folder with `realm = client` still ends up on the server.

## Commands

All commands:

```
feago init [dir] [--force]
feago build [sourceDir] [--project <file>]
feago watch [sourceDir] [--project <file>]
feago version
feago help [command]
```

Run `feago help <command>` for the full usage.

## Contributing

Contributions are very welcome. Open an issue if you hit a bug, have an idea, or want to talk through a change before writing code. 

Build locally with `go build ./cmd/feago`. There are no tests yet, so if you want to add some, even better.

## License

MIT. See [LICENSE](LICENSE).
