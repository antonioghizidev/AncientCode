# Contribuindo

Obrigado por querer contribuir. Este projeto trata um agente que **executa coisas na sua máquina**,
então a barra para uma mudança ser aceita é mais alta que num projeto comum — e o motivo está
escrito abaixo, não escondido em revisão de código.

## O invariante

**O modelo propõe. O operador decide.**

Qualquer mudança que enfraqueça isto não é contribuição aceita, mesmo que funcione e melhore uma
métrica:

- remover o gate humano antes de uma operação mutante;
- afrouxar a validação fail-closed da configuração;
- trocar execução com vetor de argumentos por shell montado a partir de texto do modelo;
- afrouxar o teto de iterações do loop;
- remover a rotulagem de saída de ferramenta como conteúdo não confiável;
- interromper o scan de exfiltração em `tool_calls`;
- permitir que segredo vá para log, disco ou request ao modelo.

Isso vale mesmo que a mudança venha com testes verdes: **os testes de segurança existem para falhar
quando a proteção é removida**, então remover a proteção derruba o gate antes de você perceber.

## Testes de segurança são guardas, não cobertura

```sh
go test -count=1 ./...
```

Um teste em `security_*_test.go` precisa **falhar se a proteção que ele cobre for removida**. Se você
mexer numa proteção, o teste correspondente tem de ficar vermelho até você removê-lo de propósito
comentando o motivo no PR. Um teste que continua verde depois da proteção sumir não é guarda: é
decoração.

## O que o PR precisa ter

1. **Spec antes de código.** Mudança que altera comportamento ou contrato passa por
   `specs/milestones/`. O método está em `specs/methodology.md`.
2. **Decisão registrada** em `docs/DECISOES.md`, com contexto e consequência — não só o "o quê".
3. **Nota de sessão** em `docs/session-<data>-<slug>.md`.
4. **Gate completo verde**:

   ```sh
   gofmt -l .
   go vet ./...
   go test -count=1 ./...
   go test -race -count=1 ./...     # com toolchain C (Linux/CI)
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 \
     go build -trimpath -ldflags="-s -w" -o ancientcode ./cmd/ancientcode
   ```

5. **Código em português**, como o resto. Comentário explica *por quê*, não *o quê*.

## Restrição de hardware — não é negociável

O alvo é um **Core 2 Duo de 2008, sem AVX, 4 GB de RAM**.

- **Não adicione dependência nativa.** `CGO_ENABLED=0` e `GOAMD64=v1` são o contrato; um build
  futuro não pode introduzir AVX por acidente.
- **Não adicione tokenizer local, ONNX, numpy, nem runtime JS.** É por isso que o projeto não
  tem essas coisas: não cabem.
- **Duas dependências diretas, no máximo.** `gopkg.in/yaml.v3` e Bubble Tea (só na camada de UI).
  Qualquer nova exige decisão registrada explicando o que a stdlib não cobre.
- `internal/*` é stdlib puro e **não importa nada de `cmd/`** — regra verificada por teste.

## Portões de documentação

Corrupção e deriva em arquivo versionado já custaram duas Correções nesta sessão, e nenhuma delas
foi pega por build ou suíte. Os gates abaixo existem e **falham com arquivo:linha**:

| Gate | Falha quando |
|---|---|
| `TestVersionedTextIsValidUTF8WithoutReplacementChars` | `.md`/`.go` versionado tem `U+FFFD` ou UTF-8 inválido |
| `TestVersionedMarkdownHasNoRawHTMLTags` | há `<tag>` cru fora de crase ou bloco cercado (quebra o markdown a partir dali) |
| `TestNoStaleOldNameInVersionedFiles` | sobrou `ancientai` em arquivo versionado |
| `TestModulePathMatchesRepo` | o `module` do `go.mod` não bate com `github.com/antonioghizidev/ancientcode` |
| `TestConfigDefaultFilenameMatchesBinaryName` | o config padrão e o nome do binário divergiram |

> **Ao editar texto em arquivo versionado, escreva bytes UTF-8.** String via PowerShell 5.1
> converte pelo code page do console e **destrói acentos** — já aconteceu (D-2026-10-02-62).

## Nomes de comandos, flags e config

Mudança de nome é breaking change: config, variável de ambiente e arquivo de dados do usuário. Foi
o caso do rename `AncientAI` → `AncientCode`, feito **sem fallback** (D-2026-10-02-63). Se sua
mudança renomeia algo que o usuário digita, diga isso no PR.

## Commit

```
feat(M<N>): <resumo imperativo em minúsculas>
```

Revise `git diff` antes. Nunca versione segredos, binários, `.env` ou `.obsidian/`.

## Reportando problema de segurança

**Não abra issue pública.** Escreva para o autor por canal privado. O achado é tratado antes de ser
divulgado, e a versão afetada é avisada.
