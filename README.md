# backend-cli

## compiling
### first step:
clone the repo
```bash
git clone sbusiedcake7945/backend-cli.git
```

### second step:
get in the folder
```bash
cd <your directory>/backend-cli
```

### third step:
compile it
for windows
```bash
go build -o backend-cli.exe .
```
for linux:
```bash
GOOS=linux GOARCH=amd64 go build -o backend-cli .
```
---

## usage
### windows:
```bash
./backend-cli.exe run <file name>.backend
```

### linux:
```bash
./backend-cli run <file name>.backend
```

___

[docs](https://backend-cli.5live.xyz)
