---
layout: default
lang: es
ref: marketplace
traduit_le: 2026-11-24
---
# Marketplace de pi-appliance

La marketplace reúne las aplicaciones que se pueden instalar y gestionar desde la consola de pi-appliance. El catálogo todavía está integrado en el programa: proponer una aplicación solo la hará disponible tras la validación de la contribución y la publicación de una nueva versión de la consola.

> **Versión experimental.** La imagen está validada en Raspberry Pi 3 B+ y Raspberry Pi 4; cada aplicación debe probarse todavía una a una en una Pi real. El acceso a los servicios desde internet aún no está disponible.

## La base de pi-appliance

La **base de pi-appliance** (Docker, Traefik, los servicios y herramientas del sistema, la consola y el comando `pia`) viene con la imagen: no está en el catálogo y no se puede desinstalar. Solo un administrador, con la cuenta creada en el primer arranque u otra cuenta de administrador, instala y gestiona las aplicaciones siguientes desde la consola.

## Aplicaciones que ya están en el catálogo

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">Cargando el catálogo…</p>
<p id="marketplace-tags-help" hidden>Toque una o varias etiquetas para ver solo las aplicaciones que tienen al menos una de ellas; tóquela de nuevo para quitarla.</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="Filtrar por etiqueta" hidden></div>
<p id="marketplace-tags-empty" hidden>Ninguna aplicación tiene estas etiquetas.</p>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>Aplicación</th><th>Descripción</th><th>Categoría</th><th>Etiquetas</th><th>Memoria</th><th>Disco</th></tr></thead>
<tbody></tbody>
</table>
</div>

Los nombres y las descripciones de las aplicaciones proceden de sus manifiestos y, por ahora, solo están escritos en francés.

Esta lista se carga desde el catálogo JSON generado a partir de los manifiestos de [`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/master/marketplace). Las consolas leen este mismo archivo en el repositorio de GitHub: una aplicación aceptada aparece en las Pi sin una nueva versión de pi-appliance. Tras cualquier cambio en un manifiesto o en `marketplace/planned.yaml`, regenere el archivo con `go run ./cmd/marketplace-json` desde la raíz del repositorio; si no, las pruebas (`go test ./...`) fallan. Si lo olvida, una acción de GitHub lo regenera en `master` tras la aceptación. El nombre del archivo contiene el MD5 de su contenido; el índice `docs/static/pi-appliance-catalog.json` indica la versión actual.

La consola gestiona varias instancias, su arranque y su parada, las actualizaciones con vuelta atrás, y la conservación o el borrado de los datos al desinstalar.

## Lo que está previsto o en estudio

Estas aplicaciones aún no se pueden instalar desde la marketplace. La lista procede del mismo catálogo JSON (`marketplace/planned.yaml`):

<ul id="marketplace-planned" hidden></ul>

**Integrado en la consola, fuera del catálogo**: [Cockpit](https://cockpit-project.org/), la administración del sistema de la Pi (registros, programas, red, discos y terminal en el navegador, para usuarios avanzados). No es un contenedor sino un software del sistema: la Marketplace de la consola lo ofrece a los administradores, solo en casa, y se entra con la cuenta de pi-appliance, sin otra contraseña (vea la [especificación]({{ '/specification.html' | relative_url }}) (en francés), sección 6.4).

Bases de datos: PostgreSQL está disponible, con la interfaz web Adminer. MongoDB (solo Pi 5), Redis, MSSQL (sin imagen para la Pi) y SQLite (un simple archivo) no se ofrecen por separado: vea la [especificación]({{ '/specification.html' | relative_url }}) (en francés), sección 6.3.

Los enlaces públicos para compartir de Etherpad también son una mejora prevista. Consulte la [hoja de ruta]({{ '/TODO.html' | relative_url }}) (en francés) para conocer su avance.

## Añadir una aplicación: todo el mundo puede contribuir

Las contribuciones están abiertas. Puede proponer una aplicación libre útil en casa (única excepción por ahora: n8n, con licencia *fair-code* «Sustainable Use License», elegida por el proyecto e indicada en su ficha), mejorar un manifiesto existente o señalar un problema. Una propuesta pasa por una pull request; se revisará y probará antes de integrarse.

### 1. Crear un manifiesto Docker Compose

Haga un fork del [repositorio pi-appliance](https://github.com/gmoulard/pi-appliance) y cree `marketplace/<identificador>/compose.yaml`. El identificador debe estar en minúsculas, empezar por una letra y contener solo letras, cifras y guiones.

El archivo es un manifiesto Docker Compose clásico con una sección `x-pi-appliance` que describe la aplicación. Ejemplo de estructura (sustituya los valores y la imagen por los de su aplicación):

```yaml
x-pi-appliance:
  name: Mi aplicación
  description: Una descripción clara para los usuarios.
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
      label: Contraseña de administrador
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

### 2. Respetar las reglas del catálogo

- Elija una imagen Docker con una **versión precisa** (no `latest` ni `stable`) y compatible con `linux/arm64`, la arquitectura de las Raspberry Pi previstas.
- Declare el servicio publicado en `service`, su puerto interno en `port` y estimaciones realistas en MB para `memory_mb` y `disk_mb`.
- El campo `icon` es un trazado SVG (`d`) en una cuadrícula de 24 × 24. El campo `login` acepta `application` o `console`; `access` acepta `public`, `users`, `home` o `admins` (administradores, en casa: reservado a las aplicaciones que controlan toda la Pi, como Portainer o Claude Code).
- Declare los parámetros en mayúsculas en `parameters`. Para los secretos, use `generate: true` en lugar de poner una contraseña en el manifiesto o en una imagen.
- Guarde los datos persistentes en volúmenes Docker con nombre. No ponga en el repositorio ni secretos, ni datos personales, ni configuración propia de un usuario. Un volumen que nunca debe copiarse en una copia de seguridad (token de una cuenta) lleva la etiqueta `pi-appliance.backup: "false"` (`labels` del volumen, como Claude Code).
- Indique la licencia de la aplicación y compruebe que su redistribución es compatible con el proyecto.
- Use solo las claves documentadas en `x-pi-appliance`: una clave desconocida hace rechazar el manifiesto.
- Evite las opciones que dan el control de toda la Pi (contenedor privilegiado, acceso a Docker, carpeta de la Pi, red del anfitrión, permisos añadidos): una nueva aplicación que las tenga no podrá instalarse desde el catálogo en línea, solo tras una nueva versión de pi-appliance. Una aplicación con acceso a Docker o que sea un contenedor privilegiado debe además declarar `access: admins`, y la consola pide confirmar su instalación.
- Una aplicación prevista pero aún no lista va en `marketplace/planned.yaml` (nombre, descripción, icono, categoría y línea de la hoja de ruta): el sitio la muestra como «Próximamente».

La definición completa de los campos y las reglas de validación están en la [especificación, sección 6.5]({{ '/specification.html' | relative_url }}) (en francés).

### 3. Comprobar y proponer

Desde la raíz del repositorio, ejecute:

```shell
go run ./cmd/marketplace-json
go test ./...
```

Las pruebas comprueban sobre todo que los manifiestos son válidos, que las imágenes tienen una versión fija y que las variables de Compose están declaradas. Si es posible, pruebe también la instalación, la parada, el reinicio y la desinstalación de su aplicación con Docker en una máquina de pruebas.

Después abra una pull request hacia `master`, con una descripción de la aplicación, los recursos necesarios, los pasos de prueba y las posibles limitaciones conocidas. Una vez aceptada la pull request en `master`, los appliances leen el nuevo catálogo en línea en un plazo de 6 horas, sin nueva versión de pi-appliance.

## Enlaces útiles

- [Código fuente de la marketplace](https://github.com/gmoulard/pi-appliance/tree/master/marketplace)
- [Especificación de los manifiestos]({{ '/specification.html' | relative_url }}) (en francés)
- [Hoja de ruta]({{ '/TODO.html' | relative_url }}) (en francés)
- [Repositorio pi-appliance](https://github.com/gmoulard/pi-appliance): abra una pull request desde su fork para proponer su contribución.
