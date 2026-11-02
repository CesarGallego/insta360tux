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

### Captura en la app oficial (v2.2.5_build25, firmware v1.4.8.8_build2, VERIFICADO salvo lo marcado)

Obtenido con `tools/interpose/` (ver su README): la app habla con la camara por IOKit
(`IOUSBInterfaceInterface::ControlRequest`), no por libusb. Trafico real al arrancar la app:

| Unit/sel | Dir | Len | Contenido observado | Interpretacion |
|---|---|---|---|---|
| 10 / 0x07 | SET | 10 | `<u64 LE microsegundos epoch>` + `0000` | VERIFICADO (valor == hora del Mac): sincroniza reloj (`SetTimestamp`) |
| 9 / 0x03 | GET | 170 | cadenas ASCII 0-terminadas: id, UUID, `v1.4.8.8_build2` | informacion del dispositivo (`GetDeviceInfo`) |
| 9 / 0x0c | GET | 32 | n.o de serie ASCII | VERIFICADO (coincide con el log de la app) |
| 9 / 0x02 | GET (1 Hz) | 52 | int32 LE en off 38 = pan (0.1 grado), off 42 = tilt (0.1 grado), u16 off 50 = zoom | VERIFICADO con la pose -0.8 / -11.6 / 158 (`f8ffffff 8cffffff ... 9e00`); es el estado sondeado por la app |
| 9 / 0x1c | GET (~2 s) | 10 | `80070000 38040000 1e00` | VERIFICADO: u32 ancho 1920, u32 alto 1080, u16 fps 30 (`GetCameraPlayRes`) |
| 9 / 0x1b | GET/SET | 2 | `3002` (= 0x0230) | mascara u16 LE de "funciones extendidas" (ver abajo) |
| 9 / 0x1e | GET/SET | 1 | `02` | auto exposicion (`EnableAutoExposure`/`GetAutoExposureStatus`), HIPOTESIS de significado del valor 2 |
| 9 / 0x19 | GET | 2 | `c305` | ISO (`GetISOValue`): 1475 coincide con "iso from uvc_extend: 1475" del log de la app |
| 9 / 0x04 | SET 262 + GET 255 | | `a5 d0 03 00 f1 32 00 ...` -> `a5 d0 03 28 aa 00 ...` | HIPOTESIS: version PTZ (`GetPTZVersionInfo`: SendCommand(4)+GetCommand); la app lo registra como "PTZ version: 170" (0xaa) |
| 9 / 0x10 | SET 255 x3 | 255 | tablas u16 crecientes (paso 4) | curva de exposicion (`SetExpouseCurve`), HIPOTESIS |
| 9 / 0x09 | GET | 2 | `0000` | compensacion de exposicion, HIPOTESIS |

Patron: para cada selector la app hace primero `GET_LEN (0x85)` y usa ese tamano; las escrituras
copian el valor al inicio de un buffer de ese tamano rellenado con ceros (p. ej. privacidad: 10 B,
`01 00 .. 00`). Los metodos `SendCommand/SendCommand2/SendCommand3` de `MacUVCCameraExtendController`
usan, respectivamente, **unit 9, 10 y 11**, con el selector como numero de comando.

#### Modos de la camara (unit 9, sel 0x02, 52 B) - VERIFICADO en bus y en hardware sin la app oficial

Sesion guiada con la app oficial (efectos fisicos confirmados por el usuario) y despues reproducido desde Go
(libusb, macOS, firmware v1.4.8.8_build2, con Photo Booth en marcha; frames de ffmpeg/avfoundation antes y despues).

SET_CUR de 52 B: **byte 0 = modo**, bytes 38..41, 42..45 y 46..49 = `1a 0e 00 00` (int32 LE 3610, significado
desconocido, la app lo envia siempre; replicado tal cual), el resto ceros. GET_CUR del mismo selector (52 B):
byte 0 = modo, byte 1 = estado (valores vistos 0x10, 0x03, 0x01; significado desconocido; no se escribe),
int32 LE en off 38 = pan y off 42 = tilt (0.1 grado), u16 en off 50 = zoom. SET y GET difieren en el diseno.

| Byte 0 | Modo | Efecto verificado |
|---|---|---|
| 0x00 | normal | control libre |
| 0x01 | seguimiento IA | la camara busca una cara y mueve el pan/tilt sola (pan pasa de -0.8 a -5.8 en 6 s); con el modo activo, `PANTILT_ABSOLUTE` pan 15 queda anulado (pan volvio a ~-1.5) |
| 0x04 | pizarra | zoom a 100 (se aleja) buscando marcadores QR; sin ellos vuelve solo a modo 0 en <11 s (el GET pasa a `00 03`) |
| 0x05 | cenital | tilt -90 (GET: -900), zoom 100; imagen mirando hacia abajo |
| 0x06 | vista de escritorio | tilt -45 (GET: -450), zoom 100; enmarca las manos/escritorio |
| 0x02, 0x03 | ? | nunca vistos; no se prueban ni se pueden escribir desde la CLI |

- **El handshake no hace falta**: los modos 1, 4, 5 y 6 funcionan escribiendo solo el bloque de 52 B en frio
  (sin `GET 9/0x14` ni `OUT 9/0x15`). Lo que precede en la app al modo 1 es `GET 9/0x14` (240 B: lista de
  float32 LE seguidos de ceros, p. ej. `0123daea 3e8edfa3 3e52e11d 3ef5bbc1 3e...`) y `OUT 9/0x15` (8 B =
  dos float32: `0de5023f aa9aa83e` = 0.512, 0.329). HIPOTESIS: es la seleccion del objetivo a seguir
  (`SetTrackObj`, centro normalizado) y no condiciona el modo; sin ella la camara busca una cara por si misma.
- Salir de un modo con `00` deja a la camara moviendose un instante (hasta ~1 s); y tras overhead/desk/pizarra
  el zoom queda en 100: el cliente debe reponerlo. Un `set` justo tras salir puede perder el zoom (visto con
  0.5 s de espera; con 1.5 s funciona) y la CLI espera 1.5 s con `--normal`.
- Sin verificar: modos 2 y 3; significado del byte 1 de estado y de las constantes 3610; efecto exacto del
  `0x15`; si el modo persiste tras desconectar el USB; comportamiento de tracking con varias personas.
- En Linux, el backend v4l2 implementa lo mismo con `UVCIOC_CTRL_QUERY` (unit 9, sel 0x02, 52 B), SIN PROBAR.

#### Mapa selector -> funcion (HIPOTESIS: deducido del desensamblado de los nombres de metodo, sin confirmar en bus salvo lo marcado arriba)

- **Unit 9**: 0x02 estado (`GetDeviceStatus`/video mode), 0x03 info, 0x04 version PTZ, 0x05/0x06 gestos
  (`EnableGestureWork` / `GestureBinding`, GET+SET), 0x07 reduccion de ruido de audio, 0x08 draw box
  (cajas de seguimiento), 0x09 compensacion de exposicion, 0x0a modo almacenamiento (`SwitchStorageMode`),
  0x0b estado de dispositivo, 0x0c serie, 0x0e restaurar valores de fabrica, 0x10 curva de exposicion,
  0x11 modo USB (`SwitchSimpleClass/NormalMode/MscReadOnly`), 0x12 velocidad de seguimiento, 0x13 estilo de
  composicion, 0x14/0x18 listas de objetos seguidos, 0x15 `SetTrackObj` (rectangulo), 0x16 pan/tilt
  relativo, 0x18 sesgo de composicion, 0x19 ISO, 0x1a pan/tilt absoluto (int32 pan?, tilt? en segundos de arco; eco),
  0x1b **mascara de funciones**, 0x1c resolucion/fps, 0x1e auto exposicion.
- **Unit 10**: 0x03 caja de seguimiento PTZ (`SetPtzTrackingBox`), 0x04 plantillas de imagen
  (`SetImageTemplate`/`DeleteTemplate`), 0x05 nombre de plantilla, 0x07 timestamp (verificado),
  0x09 `SetIQSwitch(int, bool)`, **0x0f modo privacidad** (1 B), 0x0c `RestoreFactorySet`, 0x13 `SetHostPTZ`,
  0x14 modo de captura de audio, 0x15 zona prohibida de seguimiento, 0x16 filtro, 0x17 entrar/salir de
  ajuste de zona prohibida, 0x1a **modo de seguimiento por defecto** (`SetDefaultTrackMode(int)`),
  0x1b pizarra inteligente, 0x1c correccion horizontal, 0x1d contador de apertura de U-disk, 0x1e zoom real,
  0x1f direccion de audio.
- **Unit 11 (presets)**: 0x03 numero de presets soportados (GET 1 B: **6**, VERIFICADO), 0x04 preset actual,
  0x05 ir a preset (`GotoPreset(int)`), 0x06 estado/borrado de preset (`SetPresetStatus`/`DeletePreset`),
  0x07 sincronizar preset local a la camara, 0x1b id de preset actual, 0x1c info de presets,
  0x1f area de seguimiento (`SetTrackAreaInfo`).

#### Mascara de funciones extendidas (unit 9, sel 0x1b, u16 LE; escritura = leer-modificar-escribir)

Cada `EnableExtendFuncWork(func, bool)` pone/quita el bit `1 << func`. El valor de `func` se dedujo de la tabla
de salto de `CameraInsta::InitExtendFuncStatus` y del valor real leido (`0x0230` = bits 4, 5, 9, que
coincide con los tres "init ... : 1" del log de la app: gestos, modo vertical, ajuste inteligente):

| Bit | Funcion (nombre del log de la app) | Estado |
|---|---|---|
| 0 | zoom IA (`ai_zoom_checked_`) | hipotesis |
| 2 | HDR (`enable_hdr_`) | hipotesis |
| 3 | espejo (`mirror_checked_`) | hipotesis |
| 4 | todos los gestos (`enable_all_gestures_`) | consistente con el valor leido (1) |
| 5 | modo vertical (`enable_vertical_mode_`) | consistente con el valor leido (1) |
| 6 | parametros de arranque (`enable_startup_params_`) | hipotesis |
| 7 | seguimiento con un toque (`enable_single_tap_tracking_`) | hipotesis |
| 8 | **seguimiento IA automatico** (`enable_auto_tracking_`) | hipotesis |
| 9 | ajuste inteligente (`smart_adjustment_checked_`) | consistente con el valor leido (1) |
| 10 | vertical forzado (`forced_vertical_`) | hipotesis |
| 11 | modo privacidad (`enable_privacy_mode_`) | hipotesis |
| 12 | espejo vertical (`enable_vertically_mirror_`) | hipotesis |
| 13 | baja resolucion (`enable_low_resolution_`) | hipotesis |
| 14 | zona prohibida de seguimiento (`enable_track_forbidden_area_`) | hipotesis |

Experimentos propios (camara en la pose habitual, con la app abierta y la vista previa activa; restaurado despues):

- Escribir `0x0330` (bit 8) en 9/0x1b: la camara lo acepta y lo devuelve (`3003`), pero **no se observo movimiento
  del gimbal en 8 s** (pan/tilt de sel 0x02 sin cambios). No concluyente: el seguimiento puede requerir
  que la app lo active por otro camino (unit 10 sel 0x1a / caja de seguimiento). Restaurado a `0x0230`.
- Escribir `01 00*9` en 10/0x0f (privacidad, segun el desensamblado): el SET se acepta (10 B), pero un GET
  posterior devuelve ceros y **la imagen no cambia** (la camara no se tapo). El mecanismo de lectura/efecto de la
  unit 10 queda por aclarar: probablemente hace falta la secuencia exacta de la app. Restaurado a ceros.

#### Sesion guiada de captura con la UI (VERIFICADO: una accion de UI confirmada por el usuario por cada MARK del log)

Estado base en la app: `0x0220`. Todo son control transfers UVC XU, lectura-modificacion-escritura.

| Unit / sel | Tamano | Funcion | Estado |
|---|---|---|---|
| 9 / 0x1b bit `0x004` | u16 LE | HDR | VERIFICADO (captura + hardware) |
| 9 / 0x1b bit `0x080` | | seguimiento IA de un toque (interruptor) | VERIFICADO el bit; efecto fisico no comprobado |
| 9 / 0x1b bit `0x100` | | seguimiento automatico (sigue cuando se apunta el gimbal a mano) | VERIFICADO el bit; efecto no observado (sin cara en la imagen al probar) |
| 9 / 0x1b bit `0x200` | | ajuste inteligente (activo por defecto) | VERIFICADO |
| 9 / 0x1b bit `0x400` | | bloqueo de toma vertical | VERIFICADO en hardware con stream: la imagen gira 90 grados; la camara pasa a tilt 0 y al desactivar queda en pan 0 tilt 0 (hay que reponer la pose) |
| 9 / 0x1b bit `0x020` | | sin identificar, siempre activo | se conserva siempre |
| 9 / 0x1b bit `0x010` | | interruptor maestro de gestos | HIPOTESIS consistente con el log: la app lo puso a 0 al apagar los gestos (`0x0230` -> `0x0220`) y hardware: bit aceptado y leido; efecto fisico (reconocer gestos) no comprobado |
| 9 / 0x05 | 1 B | mascara de gestos: `0x02` seguimiento, `0x04` zoom, `0x08` pizarra | VERIFICADO en captura y escritura/lectura en hardware (efecto fisico no comprobado) |
| 9 / 0x12 | 1 B | velocidad de seguimiento: `00` rapido, `01` normal ("ordinario"), `02` lento | VERIFICADO en captura y lectura/escritura en hardware |
| 5 / 0x05 | 1 B | frecuencia de red (PU_POWER_LINE_FREQUENCY estandar UVC): `01`=50 Hz, `02`=60 Hz (`00` = desactivado, `03` = auto: no se exponen) | VERIFICADO en captura y hardware |
| 1 / 0x0f | s16 | roll estandar (-100..100) = "Ajuste horizontal" (nivelado del horizonte) de la app | VERIFICADO |
| 9 / 0x1c | 10 B | solo lectura: resolucion/fps actual. 1920x1080@30 = `80070000 38040000 1e00`; 1280x720@60 = `00050000 d0020000 3c00` | VERIFICADO (informe, no se escribe) |

Hallazgos NEGATIVOS (no existe comando XU):

- **Privacidad / reposo**: no hay comando. La camara entra sola en privacidad (cabeza abajo, tilt ~ -83) cuando
  no hay ningun stream de video abierto, y despierta y reencuadra al abrirse uno. Pan/tilt/zoom enviados sin
  stream se recuerdan y se aplican al arrancar el stream. (Esto explica por que el `01 00*9` en unit 10/0x0f de
  arriba no hacia nada.)
- **Resolucion / fps**: no son un ajuste de la camara por XU; los fija el formato V4L2/UVC del stream (probe/commit).
- **Presets**: viven en la app, no en la camara (la app solo manda `unit 11 sel 0x1b` = ff... al ir a uno).
- Tras `vertical-lock` la pose vuelve a 0/0; tras `auto-tracking` activado el efecto fisico necesita una cara en la imagen (no probado).

Implementado en la libreria (`camera/feature.go`) y CLI: `feature`, `gestures`, `tracking-speed`, `flicker`.
En Linux, los backends v4l2 usan `UVCIOC_CTRL_QUERY` para las XU (SIN PROBAR) y `V4L2_CID_POWER_LINE_FREQUENCY`
(0x00980918, mismos valores que UVC) para el anti-parpadeo (SIN PROBAR).
`--keep-stream` (v4l2): mmap + `VIDIOC_STREAMON` en una goroutine para que la camara tenga un stream mientras se mueve; solo compilado, SIN PROBAR en hardware.

## Herramientas (`tools/`)

- `dump_desc.c`: descriptores USB y parseo de unidades UVC.
- `probe_uvc.c`: sondeo de solo lectura (GET_*) de controles estándar y XU.
- `ptz_test.c`, `ptz_diag.c`: prueba de escritura de pan (mueven la cámara y restauran).
- `interpose/`: interceptor IOKit/libusb para la app oficial (copia re-firmada ad hoc), `mark`, `xu` y `correlate.py`.
