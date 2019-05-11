#include <sodium.h>
#include <stdio.h>  /* printf */
#include <string.h> /* memcpy */
#include <stdlib.h> /* atoi */
#include <time.h> /* time */


#define NUMKEYS 10
void make_addr(unsigned char addr[32], unsigned char hash[64]);
int find_where(unsigned char hash[64], unsigned char besthashlist[NUMKEYS][64]);
void insert_64(unsigned char itemlist[NUMKEYS][64], unsigned char item[64], int where);
void insert_32(unsigned char itemlist[NUMKEYS][32], unsigned char item[32], int where);
