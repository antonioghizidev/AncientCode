# Sessão 2026-09-16 — Validação NVIDIA NIM no Inspiron (usuário dedicado)

> Resumo exclusivo desta sessão. Metodologia: `specs/methodology.md`.

## Objetivo da sessão

Finalizar a validação/tuning do M6 no Inspiron **como usuário dedicado `ancientai`** com
provedor **NVIDIA NIM** (config `nvidia/nemotron-3.5-lightning-30b-a3b`): shell com efeito,
write de arquivos no workdir, auditoria, e definir o pareamento de modelo/modo.

## O que foi feito

- **Setup dedicado**: binário instalado em `/opt/ancientai/bin/ancientai` (0.6.0-m6), config
  em `/home/ancientai/ancientai.yaml`, chave SSH `ancientai-wsl` validada, env replicado
  (OPENROUTER_API_KEY + ANCIENTAI_MODEL + NVIDIA_API_KEY) no `.bashrc` do usuário.
- **Correção de config pós-block**: o config instalado era o snapshot OpenRouter antigo →
  reescrito para `provider: nvidia` + `model: nvidia/nemotron-3.5-lightning-30b-a3b` +
  `api_key_env: NVIDIA_API_KEY` (slug `:free` é do OpenRouter; na NIM direta deu HTTP 404 —
  o alternativo `-30b-a3b` funcionou).
- **Workdir do dedicado**: artefatos do smoke `USUARIO` realocados para `audit-USUARIO.bak`/
  `sessions-USUARIO.bak`; `audit.jsonl` e `sessions/` recriados graváveis pelo grupo `ancientai`.
- **Pré-cold-start > 2 min**: `http.Client.Timeout` do provider subiu de 2 min para 5 min
  (D-2026-09-16-32) — o loop falhou-closed sem executar nada na legítima lentidão do NIM.
- **Smokes reais (NVIDIA, usuário `ancientai`)**:
  - pré-voo do provider/modelo: um turno plano respondeu; → fluxo y/N vivo.
  - gating de plano: `write_file` chamado direto em modo plan → recusado fail-closed (loop
    correto); nemotron não planeja em texto → pareamento recomendado `toolbox_mode: run`.
  - `write_file` executado com approve → `/opt/ancientai/code/marcador-shell.txt` (owner
    `ancientai`, 0600); `read_file` leu de volta. Audit: `ask/allow/executed` + output_hash.
  - `shell id; whoami; pwd` → **guarda de chaining M2 negou** (`command chaining is denied
    by default`); o modelo decompôs e cada comando atômico foi aprovado e executado como
    `ancientai` (~10 ms, sem `su`). Audit com 3 entradas executed.
  - Defeito D-2026-09-16-33: caminho absoluto foi re-rootado em `code_root/...` com sucesso
    falso → `resolve`/`resolveWrite` agora **recusam absoluto** (fail-closed); validado ao vivo
    no smoke seguinte.
- **Mudanças + testes**:
  - `internal/provider/openai.go`: timeout default 5 min + `TestDefaultClientTimeoutFloor`.
  - `internal/tools/files/*`: recusa de absolutos (`IsAbs`) + `TestRejectAbsolutePath`
    (read/write/list_dir + relativo continua ok).
  - `internal/tools/code/code_test.go`: `chmod 0777` explícito pós-`WriteFile` — o umask (022)
    do Linux real mascarava `0777` e a heurística `world-writable-perms` não disparava.
- **Gates**: `gofmt -l` vazio; `go vet ./...` e **suíte completa verde nos dois hosts** —
  Windows (host, serial) 14/14; **Linux real (WSL toolchain go 1.27.0 instalado em
  `/home/USUARIO/go-tools/go`)** 15/15, incluindo `TestExecBasic` sem flake em Linux.

## Decisões

- D-2026-09-16-32 (timeout provider 5 min), D-2026-09-16-33 (tools de arquivo recusam
  absolutos), D-2026-09-16-34 (NIM + nemotron → `toolbox_mode: run` com ask por tool).

## Testes

| Pacote | Foco | Status |
|---|---|---|
| `internal/provider` | piso do timeout default | ✅ |
| `internal/tools/files` | absolutos fail-closed + round-trip relativo | ✅ |
| `internal/tools/code` | determinismo do modo 0777 (umask) | ✅ |
| demais pacotes | regressão (Windows serial + Linux WSL) | ✅ |

**Total: 15 pacotes verdes no Linux (WSL go1.27.0), 14 no host Windows (serial); 0 falhas.**
Validação de hardware real: Inspiron via SSH — provider NIM ~round-trip, write/shell com efeito
como `ancientai`, auditoria `ask→allow→executed` para todos.

## Critérios de aceite

| Critério | Status |
|---|---|
| Suíte completa verde (host + Linux) | ✅ |
| Testes de segurança do milestone presentes | ✅ |
| Documentação atualizada (DECISOES + esta nota + AGENTS) | ✅ |
| Validação de comportamento real no Inspiron (NVIDIA) | ✅ |

## Próximos passos

1. `sudo install -m 755 -o ancientai -g ancientai /home/ancientai/ancientai-0.6.0-m6
   /opt/ancientai/bin/ancientai` para o artefato canônico de `/opt` ser o binário com o
   timeout 5 min + recusa de absolutos (ou considerar o binário em `/home/ancientai` como o
   deployed, com `/opt` apenas bootstrap).
2. Reenceno do `--resume` com conversa real na NIM; opcionalmente `apt`/`docker` no dedicado
   (docker já validado sem root; apt segue exigindo privilégio — D-2026-09-16-31).
3. Avaliar **M7 — Skills** (`specs/milestones/07-m7-skills.md`) — **não iniciar antes**.

## Observações / riscos

- Pareamento nemotron/NIM exige `toolbox_mode: run` (modelo não descreve plano em texto);
  manter `plan` para modelos que planejam (ex. DeepSeek/OpenRouter, validado em M6).
- Cota free do OpenRouter seguiu esgotada na sessão; NVIDIA NIM assumiu como provider do
  dedicado sem limitação observada.
- `/tmp` do WSL é tmpfs (reset entre VMs, reforçado pelo corte de energia); toolchain Go
  passou a viver em `/home/USUARIO/go-tools/go` para os gates Linux.
- `TestExecBasic` (helper de 1 s) é sensível a carga no Windows (flake pré-existente, passa
  isolado/serial); em Linux real passou estável.