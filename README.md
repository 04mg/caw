<p align="center">
  <img src="caw.svg" alt="Caw" width="200">
</p>

<h1 align="center">Caw</h1>
<p align="center"><i>cloud agentic workspace</i></p>

<p align="center">
  A terminal-based workspace manager with multi-pane terminals, tab management,
  workspace persistence, and emoji picker — powered by a Go backend and React frontend.
</p>

## Build

### Prerequisites
- [Go](https://go.dev/dl/) 1.21+
- [Node.js](https://nodejs.org/) 20+
- npm

### Frontend
```sh
cd src/frontend
npm install
npm run build
```

### Backend
From the repository root:
```sh
cd src

# Linux / macOS
go build -o ../caw .

# Windows
go build -o ../caw.exe .
```

Cross-compile from any platform:
```sh
cd src
GOOS=linux GOARCH=amd64 go build -o ../caw .
GOOS=windows GOARCH=amd64 go build -o ../caw.exe .
GOOS=darwin GOARCH=amd64 go build -o ../caw-macos .
```

## Contributing

See [CONVENTIONS.md](CONVENTIONS.md) for commit and branch naming conventions.
