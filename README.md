# go-impressao (Windows)

Serviço HTTP em Go para receber uma comanda em JSON, exibir a pré-visualização em uma página web e imprimir fisicamente em uma impressora do Windows.

## Requisitos

- Windows com PowerShell disponível
- Go instalado
- Impressora 80mm cadastrada no Windows (com nome exatamente igual ao campo `driver`)

## Como executar

```bash
go run .\cmd\go-impressao
```

O serviço inicia automaticamente em uma porta disponível e imprime no log a URL, por exemplo:

```
servidor iniciado em http://127.0.0.1:53142
```

## API

### POST /impressao/cozinha

Recebe o payload, valida, formata a comanda e retorna um `job_id` + `preview_url` para pré-visualização/impressão.

**Exemplo de requisição**

```json
{
  "tipo": "mesa",
  "numero": 0,
  "usuario": "Carlos",
  "driver": "COZINHA (Windows)",
  "modelo": "80mm",
  "impressora": "Bar (ERP)",
  "imprimir_agora": false,
  "produtos": [
    {
      "categoria": "Lanches",
      "nome": "X-Burger",
      "quantidade": 2,
      "observacoes": "sem cebola",
      "extras": [
        { "categoria": "Adicionais", "nome": "Bacon", "quantidade": 1 }
      ]
    }
  ]
}
```

**Exemplo com curl (PowerShell)**

```powershell
$body = @'
{
  "tipo": "mesa",
  "numero": 0,
  "usuario": "Carlos",
  "driver": "COZINHA (Windows)",
  "modelo": "80mm",
  "impressora": "Bar (ERP)",
  "imprimir_agora": false,
  "produtos": [
    {
      "categoria": "Lanches",
      "nome": "X-Burger",
      "quantidade": 2,
      "observacoes": "sem cebola",
      "extras": [
        { "categoria": "Adicionais", "nome": "Bacon", "quantidade": 1 }
      ]
    }
  ]
}
'@

Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:PORTA/impressao/cozinha" -ContentType "application/json" -Body $body
```

**Resposta (201)**

```json
{
  "job_id": "c2a6c3f88c2a4b0bb7c0d2f0a0f2f5a0",
  "status": "pendente",
  "preview_url": "/impressao/cozinha/preview/c2a6c3f88c2a4b0bb7c0d2f0a0f2f5a0",
  "comanda": "mesa 1\n2 - X-Burger\n  1 - Bacon\nsem cebola\n\nCarlos (COZINHA)\n04/05/2026 10:11:12\n"
}
```

### GET /impressao/cozinha/preview/{job_id}

Abre a interface web simples com a comanda formatada e um botão **Imprimir**.

### POST /impressao/cozinha/imprimir/{job_id}

Dispara a impressão de forma assíncrona (goroutine) e retorna status.

### GET /impressao/cozinha/status/{job_id}

Consulta o status do job (`pendente`, `imprimindo`, `impresso`, `erro`).

## Mensagens de erro

O serviço retorna mensagens claras em português (ex.: impressora não encontrada, impressora offline, JSON inválido, validação de campos).

Para `POST /impressao/cozinha`, o campo `modelo` agora pode ser enviado como `56mm`, `58mm` ou `80mm`. Quando omitido, o serviço mantém o comportamento atual e assume largura de `80mm`.

## Layout (80mm)

- Cabeçalho (tipo + número): tarja (reverse), negrito e preenchida até 48 colunas.
- Categorias, extras, usuário e data/hora: fonte padrão.
- `numero = 0`: permitido (não bloqueia a impressão).

## Testes

```bash
go test ./...
```
