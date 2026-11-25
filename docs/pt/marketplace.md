---
layout: default
lang: pt
ref: marketplace
traduit_le: 2026-11-25
---
# Marketplace do pi-appliance

A marketplace reúne as aplicações que se podem instalar e gerir a partir da consola do pi-appliance. O catálogo é publicado neste site: as consolas já instaladas voltam a lê-lo a cada 6 horas, e uma aplicação aceite aparece nelas sem uma nova versão do pi-appliance.

> **Versão experimental.** A imagem está validada no Raspberry Pi 3 B+ e no Raspberry Pi 4; a cada alteração, um teste automático instala cada aplicação e abre a sua página, em casa e a partir da internet, mas cada uma ainda tem de ser testada num Pi real. Os serviços também se abrem a partir da internet, no endereço do Pi (ver o [guia, em francês]({{ '/guide-utilisateur.html#services-sur-internet' | relative_url }})).

## A base do pi-appliance

A **base do pi-appliance** (Docker, Traefik, os serviços e ferramentas do sistema, a consola e o comando `pia`) vem com a imagem: não está no catálogo e não se desinstala. Só um administrador, com a conta criada no primeiro arranque ou outra conta de administrador, instala e gere as aplicações abaixo a partir da consola.

## Aplicações já no catálogo

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">A carregar o catálogo…</p>
<p id="marketplace-tags-help" hidden>Toque numa ou várias etiquetas para ver apenas as aplicações que tenham pelo menos uma delas; toque novamente para a retirar.</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="Filtrar por etiqueta" hidden></div>
<p id="marketplace-tags-empty" hidden>Nenhuma aplicação tem estas etiquetas.</p>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>Aplicação</th><th>Descrição</th><th>Categoria</th><th>Etiquetas</th><th>Memória</th><th>Disco</th></tr></thead>
<tbody></tbody>
</table>
</div>

Os nomes e as descrições das aplicações vêm dos seus manifestos e, por agora, só estão escritos em francês.

Esta lista é carregada a partir do catálogo JSON gerado a partir dos manifestos de [`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/master/marketplace). As consolas leem este mesmo ficheiro no repositório GitHub: uma aplicação aceite aparece nos Pi sem nova versão do pi-appliance. Depois de qualquer alteração a um manifesto ou a `marketplace/planned.yaml`, volte a gerar o ficheiro com `go run ./cmd/marketplace-json` a partir da raiz do repositório; caso contrário, os testes (`go test ./...`) falham. Se se esquecer, uma ação do GitHub volta a gerá-lo em `master` após a aceitação. O nome do ficheiro contém o MD5 do seu conteúdo; o índice `docs/static/pi-appliance-catalog.json` indica a versão atual.

A consola gere várias instâncias, o seu arranque e paragem, as atualizações com reversão, e a conservação ou eliminação dos dados na desinstalação.

## O que está previsto ou em estudo

Estas aplicações ainda não se podem instalar a partir da marketplace. A lista vem do mesmo catálogo JSON (`marketplace/planned.yaml`):

<ul id="marketplace-planned" hidden></ul>

**Integrado na consola, fora do catálogo**: [Cockpit](https://cockpit-project.org/), a administração do sistema do Pi (registos, programas, rede, discos e terminal no navegador, para utilizadores experientes). Não é um contentor mas um software do sistema: a Marketplace da consola propõe-no aos administradores, só em casa, e entra-se com a conta pi-appliance, sem outra palavra-passe (ver a [especificação]({{ '/specification.html' | relative_url }}) (em francês), secção 6.4).

**Fora do catálogo, Docker Compose livre**: um administrador também pode instalar uma aplicação que não está no catálogo, a partir do seu ficheiro Docker Compose, colado ou lido de um endereço web (uma página do GitHub, por exemplo). Esse serviço não é verificado nem suportado pelo projeto (ver o [guia, em francês]({{ '/guide-utilisateur.html#docker-compose-libre' | relative_url }})).

Bases de dados: o PostgreSQL está disponível, com a interface web Adminer. O MongoDB (só Pi 5), o Redis, o MSSQL (sem imagem para o Pi) e o SQLite (um simples ficheiro) não são propostos isoladamente: ver a [especificação]({{ '/specification.html' | relative_url }}) (em francês), secção 6.3.

As ligações públicas de partilha do Etherpad também são uma melhoria prevista. Consulte o [roteiro]({{ '/TODO.html' | relative_url }}) (em francês) para acompanhar o seu progresso.

## Adicionar uma aplicação: todos podem contribuir

As contribuições estão abertas. Pode propor uma aplicação livre útil em casa (única exceção por agora: n8n, sob a licença *fair-code* «Sustainable Use License», escolhida pelo projeto e assinalada na sua ficha), melhorar um manifesto existente ou assinalar um problema. Uma proposta passa por uma pull request; será revista e testada antes de ser integrada.

### 1. Criar um manifesto Docker Compose

Faça um fork do [repositório pi-appliance](https://github.com/gmoulard/pi-appliance) e crie `marketplace/<identificador>/compose.yaml`. O identificador deve estar em minúsculas, começar por uma letra e conter apenas letras, algarismos e hífenes.

O ficheiro é um manifesto Docker Compose clássico com uma secção `x-pi-appliance` que descreve a aplicação. Exemplo de estrutura (substitua os valores e a imagem pelos da sua aplicação):

```yaml
x-pi-appliance:
  name: A minha aplicação
  description: Uma descrição clara para os utilizadores.
  icon: M4 5h16v14H4z
  category: Outils
  tags: [administration]
  license: AGPL-3.0
  subdomain: outil
  service: app
  port: 8080
  memory_mb: 256
  disk_mb: 512
  login: application
  access: users
  parameters:
    - name: ADMIN_PASSWORD
      label: Palavra-passe de administrador
      generate: true

services:
  app:
    image: editeur/image:1.2.3
    restart: unless-stopped
    environment:
      ADMIN_PASSWORD: ${ADMIN_PASSWORD}
    volumes:
      - data:/var/lib/application

volumes:
  data:
```

### 2. Respeitar as regras do catálogo

- Escolha uma imagem Docker com uma **versão precisa** (não `latest` nem `stable`) e compatível com `linux/arm64`, a arquitetura dos Raspberry Pi visados.
- Declare o serviço publicado em `service`, a sua porta interna em `port`, bem como estimativas realistas em MB para `memory_mb` e `disk_mb`.
- O campo `icon` é um traçado SVG (`d`) numa grelha de 24 × 24. O campo `login` aceita `application` ou `console`; `access` aceita `public`, `users`, `home` ou `admins` (administradores, em casa: reservado às aplicações que controlam todo o Pi, como o Portainer ou o Claude Code).
- Declare os parâmetros em maiúsculas em `parameters`. Para os segredos, use `generate: true` em vez de pôr uma palavra-passe no manifesto ou numa imagem.
- Guarde os dados persistentes em volumes Docker com nome. Não ponha no repositório segredos, dados pessoais nem configuração própria de um utilizador. Um volume que nunca deve ser incluído nas cópias de segurança (token de uma conta) tem a etiqueta `pi-appliance.backup: "false"` (`labels` do volume, como o Claude Code).
- Indique a licença da aplicação e verifique que a sua redistribuição é compatível com o projeto.
- Use apenas as chaves documentadas em `x-pi-appliance`: uma chave desconhecida faz recusar o manifesto.
- Evite as opções que dão o controlo de todo o Pi (contentor privilegiado, acesso ao Docker, pasta do Pi, rede do anfitrião, permissões adicionadas): uma nova aplicação que as tenha não poderá ser instalada a partir do catálogo em linha, só depois de uma nova versão do pi-appliance. Uma aplicação com acesso ao Docker ou que seja um contentor privilegiado tem ainda de declarar `access: admins`, e a consola pede a confirmação da sua instalação.
- Uma aplicação prevista mas ainda não pronta vai para `marketplace/planned.yaml` (nome, descrição, ícone, categoria e linha do roteiro): o site mostra-a como «Em breve».

A definição completa dos campos e as regras de validação estão na [especificação, secção 6.5]({{ '/specification.html' | relative_url }}) (em francês).

### 3. Verificar e propor

A partir da raiz do repositório, execute:

```shell
go run ./cmd/marketplace-json
go test ./...
```

Os testes verificam nomeadamente que os manifestos são válidos, que as imagens têm uma versão fixa e que as variáveis do Compose estão declaradas. Se possível, teste também a instalação, a paragem, o reinício e a desinstalação da sua aplicação com o Docker numa máquina de teste.

Abra depois uma pull request para `master`, com uma descrição da aplicação, os recursos necessários, os passos de teste e eventuais limitações conhecidas. Depois de a pull request ser aceite em `master`, os appliances leem o novo catálogo em linha no prazo de 6 horas, sem nova versão do pi-appliance.

## Ligações úteis

- [Código-fonte da marketplace](https://github.com/gmoulard/pi-appliance/tree/master/marketplace)
- [Especificação dos manifestos]({{ '/specification.html' | relative_url }}) (em francês)
- [Roteiro]({{ '/TODO.html' | relative_url }}) (em francês)
- [Repositório pi-appliance](https://github.com/gmoulard/pi-appliance): abra uma pull request a partir do seu fork para propor a sua contribuição.
