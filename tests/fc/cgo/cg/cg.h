// cg.h: the C half of package cg (cg.c), for the cgo preamble and for cg.c itself.
#include <stdint.h>

typedef struct {
	int x;
	double y;
	char name[16];
	int64_t big;
} point;

int add(int a, int b);
double scale(double v, double k);
point mkpoint(int x, double y, const char *name);
double point_sum(const point *p);
void point_bump(point *p, int by);
int fails(int code);
long long sum_via_callback(int n);
int pair_diff(int a, int b);
char *join(const char *a, const char *b);
extern const char *greeting;
extern int shared_global;
int read_global(void);
