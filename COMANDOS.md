# Comandos úteis — go-impressao

## 1. Build de produção (Windows GUI / sem console)
Gera o executável em `C:\goopedir\ImpressaoGooPedir.exe`.

```powershell
# Garante que a pasta destino exista
New-Item -ItemType Directory -Force -Path C:\goopedir | Out-Null

# Build final (GUI, sem janela de console, com ícone embutido)
go build -ldflags="-H windowsgui -s -w" -o C:\goopedir\ImpressaoGooPedir.exe ./cmd/go-impressao
```

Flags:
- `-H windowsgui` — aplica o subsistema Windows GUI (não abre janela preta do cmd ao executar).
- `-s -w` — remove símbolos de debug e DWARF, reduz o tamanho do binário.
- O arquivo `cmd/go-impressao/rsrc.syso` é automaticamente embutido pelo linker (ícone da aplicação / system tray).

---

## 2. Build de debug (com console)
```powershell
go build -ldflags="-s -w" -o C:\goopedir\ImpressaoGooPedir_debug.exe ./cmd/go-impressao
```

Uso: quando quiser ver os logs do `log.Logger` em tempo real.

---

## 3. Executar em modo desenvolvimento (direto da fonte)
```powershell
go run ./cmd/go-impressao
```

Em modo dev a porta HTTP é fixa em `:21210` (veja `isGoRun()` em [main.go](cmd/go-impressao/main.go)).

---

## 4. Rodar todos os testes
```powershell
go test ./... -count=1
```

Com verbose:
```powershell
go test ./... -count=1 -v
```

Um teste específico:
```powershell
go test ./internal/config -count=1 -run TestManager
```

---

## 5. Verificação de compilação rápida
```powershell
go build ./...
```

Compila todos os pacotes sem gerar executável (bom para rodar após refactors).

---

## 6. `go vet` (análise estática)
```powershell
go vet ./...
```

---

## 7. Limpar cache e binários temporários
```powershell
go clean -cache -testcache
```

Apagar arquivo de configuração local (força reconfiguração do app):
```powershell
Remove-Item "$env:APPDATA\go-impressao\config.json" -Force -ErrorAction SilentlyContinue
```

---

## 8. Executar o build já compilado
```powershell
Start-Process "C:\goopedir\ImpressaoGooPedir.exe"
```

Ou com terminal em primeiro plano para ver logs:
```powershell
& "C:\goopedir\ImpressaoGooPedir_debug.exe"
```

---

## 9. Build multi-empresa (tenant)

O app agora consulta `GET <baseURL>/tenant` com header `tenant-id: find` e:
- se retornar **1 tenant** e nenhum estiver salvo, auto-seleciona;
- se retornar **2+ tenants**, a página `/config` exibe um `<select>` para escolher a empresa.

A sessão de tenant salva é usada nos headers das requisições externas:
- `/impressao/padrao` (teste de URL)
- `/v2/parametros` (dados da empresa)
- `/v1/impressora/servidor/` (drivers)
- `/impressao/config/go` (heartbeat)
