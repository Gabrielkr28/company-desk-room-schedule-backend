# DeskMap — API

API em Go (só biblioteca padrão) do DeskMap: mapa 3D do andar, mesas rotativas e reserva de salas. O site 3D e o plano do projeto estão em [company-desk-room-schedule-3d](https://github.com/Gabrielkr28/company-desk-room-schedule-3d) ([docs/PLANO.md](https://github.com/Gabrielkr28/company-desk-room-schedule-3d/blob/main/docs/PLANO.md)).

## Rodando

```bash
go run ./cmd/api
```

| Variável | Padrão | |
|---|---|---|
| `DESKMAP_ADDR` | `:8090` | endereço HTTP |
| `DESKMAP_DATA_DIR` | `data` | pasta com `floors/<id>/layout.json` |
| `DESKMAP_CORS_ORIGINS` | `http://localhost:5190,http://127.0.0.1:5190` | origens liberadas |

## Endpoints

| Método | Rota | |
|---|---|---|
| GET | `/api/health` | status |
| GET | `/api/floors` | andares disponíveis |
| GET | `/api/floors/{id}` | modelo resolvido (paredes, pilares, bancadas, lugares, itens, fotos); responde com `ETag` e aceita `If-None-Match` |
| PATCH | `/api/floors/{id}/photos/{photoId}` | grava a pose calibrada de uma foto |
| GET | `/api/floors/{id}/photos/{photoId}/image` | imagem da foto |

## Dados

```
data/floors/<andar>/
├── layout.json   planta editável à mão (paredes, vãos, itens de parede, pilares, bancadas, itens soltos)
├── photos.json   fotos de referência e pose calibrada de cada uma
└── photos/       imagens (fora do git: aparecem pessoas)
```

Coordenadas em metros, origem no canto noroeste, x para leste e z para sul. As bancadas são paramétricas e a API gera os lugares com IDs estáveis (`I6-S1` = bancada I6, lado sul, lugar 1). Esses IDs serão a chave das reservas.

Editar o `layout.json` recarrega o andar automaticamente. Um JSON inválido (campo desconhecido, porta fora da parede, id repetido…) não derruba nada: a API continua servindo a última versão válida e devolve o erro em `reloadError`.

## Estrutura

- `internal/floorplan`: domínio (spec → modelo resolvido, validação)
- `internal/storage`: arquivos JSON com hot reload (troca por Postgres na fase de produção)
- `internal/httpapi`: rotas, CORS e logs

## Testes

```bash
go test ./...
```
