
2.1 OK
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
2.1 KO
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaax

2.2 OK
    
2.2 KO
	
2.3 KO
2.7 KO  

5.2
#else
//comment 

#else
    #endif

#include "iso9660.h"
  #include "my_read_iso.h"

size_t g_offset;

void *to_void(void *p)
{
    return p;
}


int print_help(void)
{
    printf("help: display command help\n");
    printf("info: display volume info\n");
    printf("ls: display the content of a directory\n");
    printf("cd: change current directory\n");
    printf("tree: display the tree of a directory\n");
    printf("get: copy file to local directory\n");
    printf("cat: display file content\n");
    printf("pwd: print current path\n");
    printf("quit: program exit\n");
    return 0;
}

int print_info(void *p) 
    {
    char *tmp = p;
    struct iso_prim_voldesc *iso = to_void(tmp + HEADER_NB_SECTOR * PAGE_SIZE);
    printf("System Identifier: %.*s\n", ISO_SYSIDF_LEN,iso->syidf);
    printf("Volume Identifier: %.*s\n", ISO_VOLIDF_LEN,iso->vol_idf);
    printf("Block count: %d\n", iso->vol_blk_count.le);
    printf("Block size: %d\n", iso->vol_blk_size.le);
    printf("Creation date: %.*s\n", ISO_LDATE_LEN,iso->date_creat);
    printf("Application Identifier: %.*s\n", ISO_APP_LEN,iso->app_idf);
    return 0;
}

int print_undef(void)
{
    puts("This function is not implemented yet");
    return 0;
}

int my_errx(int code, char *msg)
{
   fprintf(stderr, "%s\n", msg);
   return code;
}

int my_warnx(int code, char *msg)
{
   fprintf(stderr, "my_read_iso: %s: unknown command\n", msg);
   return code;
}

void *read_iso(char *filename)
{
    int n = 0;

    n = open(filename, O_RDONLY);
    if (n == -1)
        return NULL;
    struct stat sa;
    if (fstat(n, &sa) == -1)
            return NULL;
    void *p = mmap(NULL, sa.st_size, PROT_READ, MAP_PRIVATE,
                    n, 0);
    char *tmp = p;
    struct iso_prim_voldesc *iso = to_void(tmp + HEADER_NB_SECTOR * PAGE_SIZE);
    g_offset = iso->root_dir.data_blk.le * PAGE_SIZE;
    return p;
}

int parse_responce(char *arg, void *p)
{
    if (strcmp("help", arg) == 0)
        return print_help();
    if (strcmp("info", arg) == 0)
        return print_info(p);
    if (strcmp("ls", arg) == 0)
        return ls(p, g_offset);
    if (strcmp("cd", arg) == 0)
        return print_undef();
    if (strcmp("tree", arg) == 0)
        return print_undef();
    if (strcmp("get", arg) == 0)
        return print_undef();
    if (strcmp("cat", arg) == 0)
        return print_undef();
    if (strcmp("pwd", arg) == 0)
        return print_undef();
    if (strcmp("quit", arg) == 0)
        return -1;
    return my_warnx(1, arg);
}

int main(int argc, char *argv[]){
    if (argc != 2)
        return my_errx(1, "usage: ./my_read_iso FILE");

    void *iso = read_iso(argv[1]);
    if (!iso)
        return my_errx(1, "usage: ./my_read_iso FILE");

    char arg[1024] = "quit";
    int res;
    while (1)
    {
        if (isatty(STDIN_FILENO))
            printf("> ");
        res = scanf("%s", arg);
        if (res < 1 || res == EOF)
            return 0;
        res = parse_responce(arg, iso);
        if (res != 0)
            return res;
    }
    return 0;
}


