# Registro de decisões — AncientAI

Registro contínuo de decisões tomadas durante o desenvolvimento (ADR lite).
Formato: cada entrada tem data, contexto, decisão, alternativa e consequência.
As decisões que alteram a SPEC devem ser refletidas lá.

---

## 2026-09-14 — Sessão M1

### D-2026-09-14-01 — Toolchain Go 1.27 (dev local) e alvo GOAMD64=v1

- **Contexto**: desenvolvimento no Windows (sem Go instalado); o alvo é um Core 2 Duo
  sem AVX. A SPEC exige que o binário não emita instruções pós-SSE2.
- **Decisão**: instalar Go 1.27.0 via winget para desenvolver; cross-compilar para o
  notebook com `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1`.
- **Alternativa**: instalar toolchain no própria notebook (muito mais lento, sem garantia
  de rede/repo no server minimal).
- **Consequência**: `GOAMD64=v1` é explícito no comando de build, dando garantia contratual
  contra regressão de instruções.

### D-2026-09-14-02 — Dependências mínimas (só gopkg.in/yaml.v3)

- **Contexto**: SPEC §2.1 e §7.3 — dependências mínimas, pinadas, revisadas.
- **Decisão**: a única dependência de terceiros é `gopkg.in/yaml.v3` (config). Todo o resto
  (HTTP, JSON, SSH, execução) usa stdlib.
- **Consequência**: superfície de supply-chain mínima; `go.sum` cobre hashes.

### D-2026-09-14-03 — Módulo Go nomeado "ancientai"

- **Decisão**: `module ancientai` (sem domínio). Repositório local sem remoto.
- **Consequência**: import paths curtos (`ancientai/internal/...`); fácil renomear depois
  para um domínio se o repo ganhar remoto.

### D-2026-09-14-04 — Chave de API lida do ambiente a cada request

- **Contexto**: SPEC §5.5 — o config só guarda o NOME do env var, nunca o valor.
- **Decisão**: `Client.Chat` lê `os.Getenv(apiKeyEnv)` por request; erros citam apenas o
  nome da variável; header Authorization nunca é logado/ecoado.
- **Consequência**: nenhuma struct guarda o segredo em memória além do header temporário.

### D-2026-09-14-05 — Máscara de segredos = prefixo 6 + "***" + sufixo 4

- **Contexto**: testes iniciais esperavam preservar mais do token; revisamos o formato.
- **Decisão**: para tokens > 12 chars, `Redact` preserva 6 chars iniciais (identificação do
  tipo: `sk-ant`, `nvapi-`, `ghp_`...) e 4 finais. Tokens ≤ 8 são totalmente mascarados;
  entre 9 e 12 preserva 4 iniciais.
- **Consequência**: identifica o tipo de credencial sem vazar o corpo; idempotente; over-mask
  seguro por construção.

### D-2026-09-14-06 — Erros com redação preservam sentinela (redactedError)

- **Contexto**: inicialmente envolvíamos o erro com `fmt.Errorf("%s", redact(...))`, o que
  quebrava `errors.Is(err, ErrProviderError)` nos testes e na superfície pública.
- **Decisão**: tipo `redactedError{msg, cause}` que redige a mensagem mas mantém a cadeia de
  `Unwrap()` — `errors.Is`/`As` continuam funcionando.
- **Consequência**: mensagens de erro exibidas nunca contêm segredos; sentença de erro
  preservada para a camada de política.

### D-2026-09-14-07 — base_url http só para loopback; run_user nunca root; model obrigatório

- **Contexto**: SPEC §5.4/§8 (deny-by-default).
- **Decisão**: `Validate` rejeita http fora de loopback; rejeita `run_user` root/vazio;
  exige `model`; exige `api_key_env` definida no ambiente no momento do Load (fail-fast);
  `shell_tool=enabled` exige `approval.default=ask`.
- **Consequência**: configuração insegura falha antes de qualquer request.

### D-2026-09-14-08 — Timeout padrão para tool desconhecida = 30s

- **Contexto**: `ToolTimeout("bogus")` retornava erro; nenhuma tool deve rodar sem deadline.
- **Decisão**: tool sem timeout configurado cai no default shell (30 s).
- **Consequência**: contrato único — toda tool tem deadline; desconhecida usa o mais comum
  (shell).

### D-2026-09-14-09 — Loop M1 single-turn, fail-closed em tool_calls

- **Contexto**: M1 é skeleton; M2 traz tools.
- **Decisão**: o loop persiste user+assistant, envia o histórico completo, redige antes de
  persistir/enviar; se o modelo responder com `tool_calls`, o turno falha com erro e NADA é
  persistido (nada de adivinhar intenção).
- **Consequência**: comportamento seguro por padrão; prompt injetado que peça execução é
  barrado pela ausência de executor.

### D-2026-09-14-10 — Store JSONL 0600 com redação na escrita E na leitura

- **Decisão**: `Append` grava em append-only (0600) e redige; `Load` re-redige por defesa em
  profundidade; `Reset` só via comando explícito (`/reset`, `--reset`).
- **Consequência**: nenhum segredo atinge o disco mesmo se uma mensagem anterior sofrer
  escrita dupla; auditoria simples por imutabilidade-por-ordem.

### D-2026-09-14-11 — Limites de leitura (fail-closed) do streaming

- **Decisão**: linha SSE limitada a 8 MiB (scanner); corpo total da resposta 16 MiB
  (configurável no cliente para teste); corpo de erro não-2xx limitado a 64 KiB.
- **Consequência**: antídoto a bombas de stream; malformed JSON/SEM `[DONE]` encerra com erro.

### D-2026-09-14-12 — go test -race indisponível no Windows (sem cgo/GCC)

- **Contexto**: `go test -race` exige toolchain C no Windows e não há gcc instalado.
- **Decisão**: não instalar toolchain C nesta sessão; rodar suíte com `-count=1`; o teste de
  concorrência do Store valida o mutex funcionalmente. `-race` fica como item de CI/release
  no Linux (o alvo).
- **Consequência**: risco residual de data race negligenciável no código atual (um mutex no
  Store), revisado na 1ª execução de CI no Linux.