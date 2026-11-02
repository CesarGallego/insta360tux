# Insta360 Link — notas de protocolo

Dispositivo USB `2e1a:4c01`, clase UVC (interfaz de control 0, unit ids abajo).
Hallazgos obtenidos en macOS con `tools/dump_desc` y `tools/probe_uvc`.

## PTZ estándar UVC (CONFIRMADO: el gimbal se mueve)

Camera Terminal, unit 1, interfaz 0. `wIndex = (unit << 8) | interfaz`, `wValue = selector << 8`.

| Control | Selector | Tamaño | Rango | Notas |
|---|---|---|---|---|
| PANTILT_ABSOLUTE | 0x0D | 8 B | pan ±522000, tilt -324000..+360000 | int32 LE pan, int32 LE tilt, en segundos de arco (3600 = 1°) |
| ZOOM_ABSOLUTE | 0x0B | 2 B | 100..400 | |
| FOCUS_ABSOLUTE | 0x06 | 2 B | 0..100 | |
| ROLL_ABSOLUTE | 0x0F | 2 B | -100..100 | |

- `SET_CUR`: bmRequestType `0x21`, bRequest `0x01`. `GET_CUR`: `0xA1` / `0x81`.
- `PANTILT_RELATIVE` (0x0E), `ZOOM_RELATIVE` (0x0C) y `ROLL_RELATIVE` (0x10): GET_INFO y SET_CUR
  hacen STALL (LIBUSB_ERROR_PIPE). Verificado en hardware: no soportados.
- El gimbal solo se vio moverse con un stream de vídeo activo (Photo Booth abierto).
  Sin vídeo, no se observó movimiento (hipótesis: reposo del gimbal).
- `GET_CUR` devuelve el valor ordenado al instante: no es feedback de posición real. El selector 0x1a
  de la unit 9 tampoco: cambia al instante tras un SET (es un eco, no la posición del motor).
- `claim_interface(0)` falla con LIBUSB_ERROR_ACCESS en macOS (la interfaz la tiene el driver de
  vídeo), pero las transferencias de control a la Camera Terminal funcionan sin reclamarla y en
  paralelo a Photo Booth con vídeo activo.

## Rangos consultados a la cámara (VERIFICADO en macOS)

GET_MIN / GET_MAX / GET_RES / GET_DEF (0x82/0x83/0x84/0x87) sobre la Camera Terminal:

| Control | MIN | MAX | RES | DEF | INFO |
|---|---|---|---|---|---|
| PANTILT_ABSOLUTE | -522000, -324000 | 522000, 360000 | 3600, 3600 (¡1°!) | 0, 0 | 0x03 |
| ZOOM_ABSOLUTE | 100 | 400 | 1 | 100 | 0x03 |
| FOCUS_ABSOLUTE | 0 | 100 | 1 | 50 | 0x07 con AF activo (bit 2 = deshabilitado por modo automático), 0x03 con AF off |
| ROLL_ABSOLUTE | -100 (0xFF9C, con signo) | 100 | 1 | 0 | 0x03 |
| FOCUS_AUTO (0x08, 1 B) | | | | 1 | 0x03; CUR=1 de fábrica |

- GET_LEN (0x85) hace STALL en todos. El RES de pan/tilt es 1°, pero la cámara acepta y devuelve
  valores de 0.3° / 0.5° en GET_CUR (eco; no se sabe si el motor los cuantiza).
- **Foco**: con autofocus activo (sel 0x08 = 1) `SET_CUR` de 0x06 hace STALL. Hay que poner 0x08 = 0
  antes; entonces el foco manual funciona y el cambio de nitidez se ve en la imagen.
- **Roll**: es un entero de 16 bits con signo; -100 y +100 se aceptan y se leen de vuelta con signo.
- Processing Unit (unit 5), solo lectura: brillo 0..100 (def 50), contraste 0..100, selectores 0x05
  (0..3), 0x06, 0x07, 0x08 (0..100), 0x0A (2000..10000, balance de blancos), 0x0B (auto, 1). No
  probamos a escribirlos.
- Orientación (visual, frames de ffmpeg): tilt positivo mira hacia arriba (techo), pan positivo
  desplaza la escena hacia la izquierda de la imagen.

## Extension units (propietarias, SIN descifrar)

| Unit | GUID | Notas |
|---|---|---|
| 9 | `faf1672d-b71b-4793-8c91-7b1c9b7f95f8` | Selectores 0x01..0x20, buffers de hasta 502 B. Sel 0x1a refleja tilt+pan (visible solo con vídeo activo). Sel 0x1c parece resolución/fps. |
| 10 | `e307e649-4618-a3ff-82fc-2d8b5f216773` | Casi todo ceros en bloques de 10 B (hipótesis: ranuras de comandos). |
| 11 | `a8bd5df2-1a98-474e-8dd0-d92672d194fa` | Igual que la 10. |

Pendiente: capturar qué escribe la app oficial (seguimiento IA, presets, privacidad, reposo).
La app (`Insta360 Link Controller.app`) usa libusb y `uvc::UVCCameraExtendController`;
plan: copia re-firmada ad hoc + dylib que intercepte `libusb_control_transfer`.

## Herramientas (`tools/`)

- `dump_desc.c`: descriptores USB y parseo de unidades UVC.
- `probe_uvc.c`: sondeo de solo lectura (GET_*) de controles estándar y XU.
- `ptz_test.c`, `ptz_diag.c`: prueba de escritura de pan (mueven la cámara y restauran).
