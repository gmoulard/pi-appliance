exemple d'usage : 
```
$ awk -f verifCode.awk monfichier.c
report verif code :  monfichier.c
1 : 2.5-BEGIN LINE EMPTY :
5 : 2.1-lg>80 :aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaax
8 : 2.7-ESPACE END LINE :    <
10 : 2.2-TAB :
12 : 2.7-ESPACE END LINE :2.7 KO  <
16 : 2.7-ESPACE END LINE ://comment <
19 : 5.1 #PreProc start # :    #endif
18 : 5.2 #PreProc else endif alwas comment :#else
19 : 5.2 #PreProc else endif alwas comment :    #endif
22 : 5.1 #PreProc start # :  #include "my_read_iso.h"
46 : 2.7-ESPACE END LINE :int print_info(void *p) <
47 : 6.3-BRACE START BAD COLUMN :    {
118 : 6.2-BRACE OWN LINE :intmain(intargc,char*argv[]){
118 : 6.3-BRACE START BAD COLUMN :int main(int argc, char *argv[]){
142 : 2.5-END LINE EMPTY :
nbPbs:  15

``` 
