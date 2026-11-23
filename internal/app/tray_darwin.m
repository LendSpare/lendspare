// The Cocoa half of the macOS menu-bar tray: a coloured-dot NSStatusItem and an NSMenu
// rebuilt just before AppKit shows it. See tray_darwin.go for the Go side and tray_darwin.h
// for why this lives in its own translation unit instead of the cgo preamble.
#import "tray_darwin.h"

// cgo generates real C symbols for //export'd Go functions, but the header declaring them
// (_cgo_export.h) is only generated for Go files, so plain .m files in the package forward
// declare the ones they call by hand.
extern void goLendSpareTrayClick(int tag);
extern void goLendSpareTrayTick(void);
extern void goLendSpareMenuNeedsUpdate(void);

static NSStatusItem *statusItem;
static NSMenu *trayMenu;

@interface MeshMenuTarget : NSObject<NSMenuDelegate>
- (void)itemClicked:(id)sender;
@end

@implementation MeshMenuTarget
- (void)itemClicked:(id)sender {
    NSMenuItem *item = (NSMenuItem *)sender;
    goLendSpareTrayClick((int)item.tag);
}
// Rebuild just before AppKit shows the menu, not on a timer - so we never mutate it
// while it is open (the macOS equivalent of Windows building a fresh popup per click).
- (void)menuNeedsUpdate:(NSMenu *)menu {
    goLendSpareMenuNeedsUpdate();
}
@end

static MeshMenuTarget *menuTarget;

static NSImage *meshDotImage(int r, int g, int b) {
    NSImage *img = [[NSImage alloc] initWithSize:NSMakeSize(16, 16)];
    [img lockFocus];
    NSColor *fill = [NSColor colorWithCalibratedRed:r / 255.0 green:g / 255.0 blue:b / 255.0 alpha:1.0];
    NSColor *rim = [NSColor colorWithCalibratedRed:(r / 255.0) * 0.55 green:(g / 255.0) * 0.55 blue:(b / 255.0) * 0.55 alpha:1.0];
    NSBezierPath *circle = [NSBezierPath bezierPathWithOvalInRect:NSMakeRect(1, 1, 14, 14)];
    [fill setFill];
    [circle fill];
    circle.lineWidth = 1.0;
    [rim setStroke];
    [circle stroke];
    [img unlockFocus];
    img.template = NO;
    return img;
}

void lendspare_tray_init(void) {
    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory]; // menu-bar only, no Dock icon
    statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    menuTarget = [MeshMenuTarget new];
    trayMenu = [[NSMenu alloc] init];
    trayMenu.delegate = menuTarget;
    trayMenu.autoenablesItems = NO;
    statusItem.menu = trayMenu;
    [NSTimer scheduledTimerWithTimeInterval:1.0 repeats:YES block:^(NSTimer *t) {
        goLendSpareTrayTick();
    }];
}

void lendspare_tray_set_icon(int r, int g, int b, const char *tip) {
    statusItem.button.image = meshDotImage(r, g, b);
    statusItem.button.toolTip = [NSString stringWithUTF8String:tip];
}

void lendspare_tray_clear_menu(void) {
    [trayMenu removeAllItems];
}

void lendspare_tray_add_item(const char *label, int tag, int enabled, int checked) {
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:[NSString stringWithUTF8String:label]
                                                   action:@selector(itemClicked:)
                                            keyEquivalent:@""];
    item.target = menuTarget;
    item.tag = tag;
    item.enabled = enabled ? YES : NO;
    item.state = checked ? NSControlStateValueOn : NSControlStateValueOff;
    [trayMenu addItem:item];
}

void lendspare_tray_add_separator(void) {
    [trayMenu addItem:[NSMenuItem separatorItem]];
}

void lendspare_tray_run(void) {
    [NSApp run];
}
