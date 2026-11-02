// xu get|set <unit> <sel> [hex]  (herramienta de prueba, libusb)
#include <libusb-1.0/libusb.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
int main(int argc,char**argv){
  if(argc<4) return 2;
  libusb_init(NULL); libusb_device_handle*h=libusb_open_device_with_vid_pid(NULL,0x2e1a,0x4c01);
  if(!h){puts("open fail");return 1;}
  int unit=atoi(argv[2]), sel=strtol(argv[3],0,0);
  uint8_t l[2]={0}; libusb_control_transfer(h,0xA1,0x85,sel<<8,unit<<8,l,2,500); int len=l[0]|l[1]<<8;
  uint8_t b[512]={0};
  if(!strcmp(argv[1],"get")){ int r=libusb_control_transfer(h,0xA1,0x81,sel<<8,unit<<8,b,len,500); printf("len=%d r=%d ",len,r); for(int i=0;i<r&&i<len;i++)printf("%02x",b[i]); puts(""); }
  else { const char*s=argv[4]; int n=strlen(s)/2; for(int i=0;i<n;i++){unsigned v;sscanf(s+2*i,"%2x",&v);b[i]=v;} int r=libusb_control_transfer(h,0x21,0x01,sel<<8,unit<<8,b,n,500); printf("set r=%d\n",r);}
  libusb_close(h); return 0; }
