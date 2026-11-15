#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include "cg.h"
#include "_cgo_export.h"

const char *greeting = "hello from C";
int shared_global = 17;
int read_global(void) { return shared_global; }

int add(int a, int b) { return a + b; }
double scale(double v, double k) { return v * k; }

point mkpoint(int x, double y, const char *name) {
	point p;
	memset(&p, 0, sizeof p);
	p.x = x;
	p.y = y;
	strncpy(p.name, name, sizeof p.name - 1);
	p.big = (int64_t)x << 40;
	return p;
}

double point_sum(const point *p) { return p->x + p->y + (double)(p->big >> 40); }
void point_bump(point *p, int by) { p->x += by; p->y += by; }

int fails(int code) {
	if (code == 0) {
		errno = 0;
		return 7;
	}
	errno = code;
	return -1;
}

// C calling back into Go (goSquare, exported by export.go)
long long sum_via_callback(int n) {
	long long s = 0;
	for (int i = 1; i <= n; i++) s += goSquare(i);
	return s;
}

int pair_diff(int a, int b) {
	struct goPair_return r = goPair(a, b);
	return r.r0 - r.r1;
}

char *join(const char *a, const char *b) {
	size_t n = strlen(a), m = strlen(b);
	char *s = malloc(n + m + 2);
	memcpy(s, a, n);
	s[n] = '+';
	memcpy(s + n + 1, b, m + 1);
	return s;
}
