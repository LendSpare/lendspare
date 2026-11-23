// Declarations for tray_darwin.m, included from tray_darwin.go's cgo preamble. Kept in a
// real header (rather than function bodies inside the preamble) so cgo's generated
// _cgo_export.c - which re-includes the preamble verbatim - does not link in a second
// definition of everything here; see tray_darwin.m.
#import <Cocoa/Cocoa.h>

void lendspare_tray_init(void);
void lendspare_tray_set_icon(int r, int g, int b, const char *tip);
void lendspare_tray_clear_menu(void);
void lendspare_tray_add_item(const char *label, int tag, int enabled, int checked);
void lendspare_tray_add_separator(void);
void lendspare_tray_run(void);
