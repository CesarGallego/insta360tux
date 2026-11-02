# Handoff — insta360tux

Objetivo: controlar el gimbal (pan/tilt/zoom) de una **Insta360 Link** desde Linux,
partiendo de un análisis del protocolo hecho en un Mac (Apple Silicon, SIP activo).

Estado a 2026-11-02. Detalles técnicos en `PROTOCOL.md`.

## Qué está hecho y verificado

- La cámara aparece como UVC `2e1a:4c01` en macOS.
- Descriptores volcados (`tools/dump_desc`): Camera Terminal (unit 1), Processing Unit (5)
  y tres extension units propietarias (9, 10, 11) en la interfaz 0.
- **PTZ estándar UVC funciona y mueve el gimbal** (confirmado visualmente en Photo Booth):
  - `PANTILT_ABSOLUTE` sel 0x0D, 8 bytes (int32 LE pan + int32 LE tilt, segundos de arco).
  - `ZOOM_ABSOLUTE` sel 0x0B, 100..400. Foco 0x06 (0..100). Roll 0x0F (-100..100).
- Solo se observó movimiento **con un stream de vídeo activo**. Sin vídeo no se vio nada
  (hipótesis: el gimbal está en reposo si nadie usa la cámara; no demostrado).
- Lectura de solo lectura de todas las XU (`tools/probe_uvc`): selectores, longitudes y valores.

## Lo que NO sabemos / errores a evitar

- `GET_CUR` de pan/tilt devuelve el valor **ordenado**, no la posición real: no sirve
  como prueba de movimiento. Primero se dio por confirmado con eso y era incorrecto.
- `PANTILT_RELATIVE` (0x0E) está en el descriptor pero no responde.
- El tilt cambió solo entre pruebas (-83° → -42°): parece una pose de reposo que se corrige
  al activar la cámara. Sin confirmar.
- Las extension units no están descifradas; los selectores 0x1a (unit 9) reflejan tilt+pan
  solo con vídeo activo. Units 10 y 11: bloques de 10 bytes casi todo ceros.
- No se ha probado nada en Linux todavía.

## Por qué no hay captura USB en este Mac

No existe interfaz `XHC*` para `tcpdump`/Wireshark (Apple Silicon + SIP). Descartado
sniffear el bus aquí. Alternativa elegida: interceptar en la app oficial.

## Siguientes pasos (en orden)

1. **[HECHO: interceptor] [PENDIENTE: sesion de captura con la UI]** Interceptor para descifrar las XU (ver `tools/interpose/README.md`: la app usa IOKit, no libusb). `Insta360 Link Controller.app` usa
   libusb y `uvc::UVCCameraExtendController`; tiene hardened runtime con
   `disable-library-validation`. Plan: copiar la app a un directorio del proyecto,
   re-firmarla ad hoc (`codesign --force --deep -s -`), cargar un dylib que intercepte
   `libusb_control_transfer` y vuelque requestType/bRequest/wValue/wIndex/datos mientras
   se usan las funciones de la UI (seguimiento IA, presets, privacidad, reposo).
   **No modificar la app original de `/Applications`.**
   Alternativa si no usa libusb: VM Windows con passthrough + Wireshark/USBPcap.
2. **Herramienta Linux**: probar `v4l2-ctl --set-ctrl pan_absolute=..,tilt_absolute=..,zoom_absolute=..`
   con `uvcvideo`. Si no expone los controles, script con `pyusb`/`libusb` reutilizando
   las transferencias de `tools/ptz_test.c`. Recordar mantener el stream abierto.
3. Documentar las XU descifradas en `PROTOCOL.md`.

## Estado del repositorio y acceso

- Repo privado: https://github.com/CesarGallego/insta360tux (rama `main`).
- El remoto `origin` usa SSH (`git@github.com:CesarGallego/insta360tux.git`) y funciona.
- SSH fallaba porque este Mac no tenía clave para GitHub (la clave de host ya estaba bien).
  Se generó `~/.ssh/id_ed25519_github` (sin passphrase), se añadió un bloque `Host github.com`
  a `~/.ssh/config` y se registró la clave pública en la cuenta como "MacBook Air de César".
  El token de `gh` tiene ahora también el scope `admin:public_key`.

## Cómo reproducir las herramientas (macOS)

```sh
brew install libusb
cd tools
cc -o dump_desc dump_desc.c -I/opt/homebrew/include -L/opt/homebrew/lib -lusb-1.0
./dump_desc          # descriptores
./probe_uvc          # sondeo solo lectura
./ptz_diag           # MUEVE el pan +30° y restaura; abrir Photo Booth antes
```

Los binarios están en `.gitignore`.

## Actualización: CLI en Go (2026-11-02)

- `camera/` (librería) + `cmd/insta360/` (CLI, `insta360 help-agent` para la referencia completa).
- Backends: libusb (gousb, macOS/Linux, necesita `brew install pkgconf libusb`) y v4l2 (Linux, Go puro, prioridad 100).
- Verificado en hardware: movimiento real desde Go (ver sección de pruebas de hardware abajo).
- Sin probar: v4l2 en Linux real. (Movimiento, foco, roll y rangos reales: ver la sección de pruebas de hardware abajo.)
- Pendiente: interceptor libusb para descifrar las XU (seguimiento IA, presets, privacidad).

## Pruebas de hardware en macOS (2026-11-02, libusb, Photo Booth con vídeo activo)

Método: antes/después con `ffmpeg -f avfoundation` (frames de la propia Link, que se puede abrir a
la vez que Photo Booth) comparando imágenes; la cámara restaurada a pan -0.8, tilt -11.6, zoom 158,
foco 0, roll 0, AF on.

| Prueba | Resultado |
|---|---|
| `set` pan/tilt +-10 | OK, el encuadre cambia (pan+: escena a la izquierda; tilt+: mira al techo) |
| `set --zoom 200`, `zoom N` | OK, GET_CUR coincide e imagen más cerrada |
| `move`, `center`, `home` | OK (el frame tras `center` mira al techo: tilt 0) |
| `--clamp` | OK (`zoom 999` -> 400); no se llevó pan al tope por seguridad |
| fuera de rango / NaN | exit 4 con JSON de error (pan 500, zoom 999, tilt -91, nan) |
| `--dry-run` | OK: `get` y SSIM de frames sin cambios |
| foco | **Bug encontrado**: con AF activo el SET hace STALL (exit 5). Corregido: `SetFocus` apaga AF; `--autofocus[=bool]`. Foco 50 vs 100 cambia la nitidez (visible) |
| roll -100..100 | Se acepta y se lee con signo. Ver nota abajo sobre confirmación visual |
| rangos reales | `info` consulta GET_MIN/MAX/RES/DEF (nuevo `RangeQuerier`, `ranges_source`) y coinciden con los valores por defecto |
| PANTILT_RELATIVE y otros selectores estándar relativos | STALL: no soportados |
| `--device` `1.10`, `2e1a:4c01` (mayúsculas también), selector inexistente | OK / exit 3 |
| claim de la interfaz 0 | `claim_interface` falla con ACCESS en macOS pero el control funciona con Photo Booth abierto, sin error de ocupado; llamadas CLI repetidas en bucle OK |

Sin verificar: movimiento sin stream de vídeo (no se cerró Photo Booth); selector por número de serie
(la cámara no expone serial); sub-grado en pan/tilt (RES=1°); extremos +-145 / -90..100 físicos;
efecto visual exacto de roll (confirmación visual por el usuario pendiente); v4l2 en Linux real
(en Linux habrá que apagar `focus_automatic_continuous` antes de `focus_absolute`, el backend v4l2
no lo hace aún); las extension units siguen sin descifrar; PU (brillo, etc.) sin escribir.

## Interceptor y XU (2026-11-02)

- `tools/interpose/` (xulog.c, build.sh, run.sh, mark.c, xu.c, correlate.py, README): copia ad hoc de la app en `work/`
  (gitignored) con dylib que registra todas las peticiones de control (IOKit). Funciona con SIP activo.
- Hallazgos y mapa selector->funcion en `PROTOCOL.md`. Falta una sesion guiada con la UI (no se pudo pulsar
  botones: sin permiso de accesibilidad / computer-use). Sin subcomandos de CLI nuevos por falta de correlacion segura.

## Modos de la camara (2026-11-02)

`insta360 mode [normal|tracking|whiteboard|overhead|desk]`, `camera.ModeController` (libusb, fake y v4l2 via
`UVCIOC_CTRL_QUERY` sin probar). Verificado en hardware sin la app oficial; no hace falta el handshake 0x14/0x15.
`set/move --normal` suelta el modo antes de mover. Detalle y pendientes en `PROTOCOL.md` (seccion "Modos de la camara").

## Features, gestos, velocidad y anti-parpadeo (2026-11-02)

- `camera/feature.go` (FeatureController, GestureController, TrackingSpeedController, PowerLineController) en libusb, fake y
  v4l2 (sin probar); CLI `feature`, `gestures`, `tracking-speed`, `flicker`, todo en `get --all` y `help-agent`.
- VERIFICADO en hardware (macOS, Photo Booth con stream): escribir, leer y restaurar cada bit (HDR, 0x080, 0x100, 0x200, 0x010,
  0x400), mascara de gestos, velocidad, 50/60 Hz. `vertical-lock` gira la imagen 90 grados y deja la pose en 0/0 al apagarlo.
- VERIFICADO el efecto fisico (macOS, 2026-11-02): seguimiento con un toque (boton tactil entra/sale), gestos de seguimiento y de zoom
  (bit 0x010 maestro + mascara 0x05). NO reproducible por CLI: seguimiento automatico (0x100), que se dispara con el movimiento manual
  de la app (unit 9 sel 0x16, sin descifrar); `mode tracking` si da seguimiento directo.
- NO verificado: gesto de pizarra con marcadores QR; todo lo de Linux incl. `--keep-stream` (`camera/stream_linux.go`, solo compilado).
- Hallazgos negativos (privacidad, resolucion/fps, presets) y tabla de selectores en `PROTOCOL.md`.
- Estado final de la camara: 0x0220, gestos 0, velocidad normal, 50 Hz, roll 0, modo normal, pan -0.8 tilt -11.6 zoom 158.

## Lista de comprobaciones para la primera prueba en Linux

Orden recomendado; marcar cada punto como verificado o roto en este fichero.

1. **Build:** `go build ./...` (v4l2 no necesita cgo; libusb necesita `libusb-1.0-dev` y `pkg-config`). `go vet ./... && go test ./...`.
2. **Permisos:** `ls -l /dev/video*`; añadir el usuario al grupo `video` o una regla udev para `2e1a:4c01`. Sin permisos debe salir exit 7.
3. **Detección:** `insta360 list` y `insta360 info`. Comprobar que elige el backend `v4l2` (prioridad 100) y que `ranges_source` es `device`. Elegir el nodo de captura correcto (no el de metadatos).
4. **Lectura sin stream:** `insta360 get --all`. Anotar qué controles faltan (roll no existe en V4L2; foco depende del driver).
5. **Stream:** abrir un stream (`ffplay /dev/videoN` o `v4l2-ctl --stream-mmap`), luego `set --pan 10`, `set --tilt`, `set --zoom 200`, `move`, `center`. Verificar movimiento real, no solo la lectura.
6. **Sin stream:** repetir `set --pan 40` sin stream y luego abrirlo: la cámara debe aplicar la pose al arrancar (en macOS lo hizo).
7. **Foco:** `set --focus 50` falla con autofoco activo; el backend v4l2 aún no apaga `focus_automatic_continuous` antes (pendiente de implementar y probar).
8. **Extension units por `UVCIOC_CTRL_QUERY`:** probar `mode tracking|normal|overhead|desk`, `feature hdr on|off`, `gestures`, `tracking-speed`. Verificar que `uvcvideo` acepta las XU 9/10/11 (puede requerir un mapeo de controles dinámico o `UVCIOC_CTRL_MAP`). Restaurar: máscara `0x0220`, gestos 0, velocidad normal, modo normal.
9. **Antiparpadeo:** `flicker 60` y `flicker 50` (control V4L2 estándar).
10. **`--keep-stream`:** `set --pan 20 --keep-stream --resolution 1280x720 --fps 30`; comprobar que el gimbal se mueve con él y que se suelta al terminar. Verificar el reintento de `DQBUF` (EAGAIN).
11. **Backend libusb en Linux:** `--backend libusb`; necesita desligar `uvcvideo` de la interfaz de control y puede romper el stream; comprobar y documentar.
12. **Vertical lock:** al desactivarlo la cámara queda en pan 0 / tilt 0; reponer la pose.

Pose y estado de referencia al terminar: máscara `0x0220`, pan -0.8, tilt -11.6, zoom 158, modo normal, 50 Hz, roll 0 (si existe).
