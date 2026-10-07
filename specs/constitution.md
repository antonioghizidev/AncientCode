# Constituição do AncientCode

Regras **não-negociáveis**. Nenhuma mudança pode violá-las; qualquer exceção exige revisão
explícita de segurança e registro em `docs/DECISOES.md`.

## 1. Segurança por padrão (deny-by-default)

- Nada é permitido por omissão. Opções de segurança só podem ser **relaxadas explicitamente**,
  nunca pelo default.
- Configuração insegura (http fora de loopback, `run_user` root, aprovação automática
  irreversível) falha na validação, antes de qualquer execução.

## 2. Confiança zero no conteúdo do modelo e das ferramentas

- A saída do LLM e a saída de ferramentas são **dados não confiáveis**: nunca executados
  diretamente; sempre passam por parser → políticas → aprovação humana.
- Ferramenta mutável nunca roda sem aprovação explícita (salvo config assertiva e revisada).

## 3. Segredos jamais saem

- Chaves/senhas/tokens vivem em variáveis de ambiente ou secret manager. Nunca em repositório,
  logs, histórico, mensagens de erro ou artefatos.
- Redação obrigatória antes de persistir, logar ou enviar ao modelo (`internal/redact`).

## 4. Testes são critério de aceite

- Todo milestone escreve testes unitários para o que for relevante e **obrigatoriamente para
  todas as questões de segurança relevantes** ao escopo.
- **Critério de aceite obrigatório: toda a suíte do projeto passa** (`go test -count=1 ./...`)
  ao final de cada milestone. Sem suíte verde não há conclusão.

## 5. Qualidade estática obrigatória

- `gofmt`, `go vet`, `go build ./...` limpos antes de qualquer commit.
- Análise de vulnerabilidades (`govulncheck`) e lints de segurança (`gosec`) assim que o CI
  estiver disponível.

## 6. Decisões documentadas

- Toda decisão que afete comportamento, interface ou segurança é registrada em
  `docs/DECISOES.md` com data, contexto, decisão e consequência.

## 7. Compatibilidade com o alvo

- Binário para o notebook: `GOOS=linux GOARCH=amd64 GOAMD64=v1`, `CGO_ENABLED=0`. É proibido
  introduzir instruções pós-SSE2 (AVX/AVX2) no artefato final.

## 8. Dependências mínimas e revisadas

- Só entra dependência que a stdlib não resolve; revisão manual do diff de cada versão.
- `go.sum` sempre versionado.

## 9. Auditoria e rastreabilidade

- Toda execução relevante e toda recusa de segurança são registradas (JSONL append-only,
  rotação configurável). A auditoria é usuária do sandbox, nunca root.

## 10. Sem downgrade silencioso

- Não se enfraquece proteção "para os testes passarem" nem se desativa verificação TLS.
- Melhor falhar fechado (fail-closed) do que adivinhar intenção.