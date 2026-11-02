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

1. **Interceptor de libusb** para descifrar las XU. `Insta360 Link Controller.app` usa
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
