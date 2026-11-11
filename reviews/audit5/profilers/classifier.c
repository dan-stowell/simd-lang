#define main original_main
#include "../../../tools/simdratio.c"
#undef main
int audit_classify(const unsigned char *p) { return classify(p); }
