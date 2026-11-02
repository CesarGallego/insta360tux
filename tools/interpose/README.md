# Interceptor de las peticiones UVC de Insta360 Link Controller (macOS, arm64)

Objetivo: ver exactamente que escribe/lee la app oficial en las extension units 9/10/11 para
documentarlo en `PROTOCOL.md`. La app original de `/Applications` no se toca: se trabaja sobre una
copia en `work/` (ignorado por git).

## Como funciona (hallazgos)

- La app **enlaza libusb de forma estatica**, pero **no la usa para UVC**: el control de la camara
  (`uvc_camera_control_imp.mm`, `MacUVCCameraControl`) usa **IOKit** (`IOCreatePlugInInterfaceForService`
  -> `QueryInterface` -> `IOUSBInterfaceInterface::ControlRequest`).
- `DYLD_INTERPOSE` solo veria `IOCreatePlugInInterfaceForService`. `xulog.dylib` lo interpone, envuelve
  el `QueryInterface` del plugin y sustituye la vtable COM de la interfaz/dispositivo por una copia
  con `ControlRequest`, `ControlRequestTO`, `ControlRequestAsync[TO]` y `DeviceRequest[TO|Async]`
  enganchadas (slots 24/25/37/38 de `IOUSBInterfaceInterface` y 26/27/30/31 de `IOUSBDeviceInterface`;
  validados en hardware, 13 y 26 con un programa de prueba).
- Ademas parchea en memoria `libusb_control_transfer` (libusb estatico, solo se aplica si los 16 bytes de
  la entrada coinciden con v2.2.5_build25). Hasta ahora la app no lo ha llamado nunca.
- Para que `DYLD_INSERT_LIBRARIES` funcione la copia se re-firma **ad hoc sin hardened runtime** y sin
  entitlements restringidos (`application-identifier`, `team-identifier`, `system-extension.install`,
  perfil de aprovisionamiento): AMFI mataria el proceso con ellos. No hace falta tocar SIP/AMFI.
  Consecuencias: la copia no puede instalar la camara virtual (system extension) y `crashpad_handler`
  falla al cargar libs (cosmetico).

## Uso

```sh
brew install libusb                       # solo para la herramienta xu
tools/interpose/build.sh --resign         # compila dylib + mark + xu; copia y re-firma la app en work/
tools/interpose/run.sh                    # lanza la copia instrumentada (cerrar antes la app original)
work/mark "toggle tracking OFF"           # en otra terminal: marca en el log (INSTA360_XU_LOG por defecto work/logs/xu.log)
python3 tools/interpose/correlate.py      # resumen: marcas + escrituras + lecturas que cambian
```

`work/logs/xu.log`: una linea por peticion, `epoch.us DIR tag rt= req= val= idx= len= ret= unit= sel= data=`.
`OUT` = datos enviados (antes de la llamada), `OUT-RET` = resultado, `IN` = datos recibidos,
`IN-REQ` = peticion de lectura enviada. `INFO` son mensajes del interceptor. El log de la app
(`work/logs/app.out`, mismo reloj local) nombra cada accion en `camera_insta.cc`.

`work/xu get|set <unit> <sel> [hex]`: lectura/escritura cruda de una XU con libusb (para pruebas).
**Cuidado**: los `set` mueven o reconfiguran la camara de verdad.

## Sesion de captura guiada (para el usuario)

Con la app instrumentada abierta y la camara en la pose habitual, en otra terminal, para cada paso:
`work/mark "<etiqueta>"`, hacer UNA accion en la UI, esperar 3 s. Orden sugerido:

1. `mark baseline` (no tocar nada 5 s).
2. Seguimiento IA: `mark tracking-on`, activarlo; `mark tracking-off`, desactivarlo.
   (Probar tambien el interruptor "seguimiento con un toque" y los modos/velocidad de seguimiento.)
3. Gestos: `mark gestures-off` / `gestures-on` (interruptor general).
4. Modo privacidad: `mark privacy-on` / `privacy-off`.
5. Presets: `mark preset-save-1` (guardar posicion actual), mover, `mark preset-goto-1`, `mark preset-delete-1`.
6. Reposo: `mark sleep` / `mark wake` (si la app lo ofrece).
7. HDR, espejo, composicion/encuadre, velocidad de seguimiento, zoom IA: un `mark` por interruptor.
8. Arrancar/parar vista previa: `mark preview-start` / `mark preview-stop` (aqui es donde la camara
   busca una cara sola: comprobar cual de los interruptores de arriba lo evita).

Despues: `python3 tools/interpose/correlate.py` y pegar la salida (los `OUT` bajo cada MARK son
las ordenes). El bit/valor que cambia en la unit 9 sel 0x1b entre marcas identifica la funcion.
