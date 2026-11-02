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
- `PANTILT_RELATIVE` (0x0E) está en el descriptor pero la cámara no responde a GET_INFO.
- El gimbal solo se vio moverse con un stream de vídeo activo (Photo Booth abierto).
  Sin vídeo, no se observó movimiento (hipótesis: reposo del gimbal).
- `GET_CUR` devuelve el valor ordenado al instante: no es feedback de posición real.

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
