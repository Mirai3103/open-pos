import * as React from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { Coffee, Clock, Lock, LogOut, Menu, Volume2, VolumeX } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { useSessionStore } from "@/stores/use-session-store";
import { useLock, useSignOut } from "@/features/auth/api/use-auth";
import { useSound, playTapChirp } from "@/lib/sound";
import { visibleNavItems } from "./nav-items";

const WORKSPACE_LABELS: Record<string, string> = {
  cashier: "Quầy thu ngân",
  preparation: "Khu pha chế",
  manager: "Quản lý",
};

export function PosHeader() {
  const routerState = useRouterState();
  const currentPath = routerState.location.pathname;
  const displayName = useSessionStore((s) => s.displayName);
  const workspace = useSessionStore((s) => s.workspace);
  const capabilities = useSessionStore((s) => s.capabilities);
  const lock = useLock();
  const signOut = useSignOut();
  const { enabled: soundEnabled, toggle: toggleSound } = useSound();
  const [navOpen, setNavOpen] = React.useState(false);
  const navItems = visibleNavItems(capabilities);

  const [time, setTime] = React.useState<string>("");

  React.useEffect(() => {
    const updateTime = () => {
      const now = new Date();
      setTime(
        now.toLocaleTimeString("vi-VN", {
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        })
      );
    };
    updateTime();
    const timer = setInterval(updateTime, 1000);
    return () => clearInterval(timer);
  }, []);

  return (
    <header className="flex h-16 w-full items-center justify-between border-b border-border bg-card px-3 sm:px-4 shrink-0 select-none">
      {/* Brand & Navigation */}
      <div className="flex items-center gap-2 sm:gap-4 lg:gap-5">
        <Button
          type="button"
          variant="outline"
          size="icon"
          onClick={() => {
            playTapChirp();
            setNavOpen(true);
          }}
          className="h-11 w-11 rounded-xl lg:hidden"
          aria-label="Mở menu điều hướng"
        >
          <Menu className="h-5 w-5" />
        </Button>

        <Link to="/" className="flex items-center gap-2.5 font-bold text-foreground">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-xs">
            <Coffee className="h-5 w-5" />
          </div>
          <div className="flex flex-col">
            <span className="text-base font-bold leading-tight tracking-tight">POS Cafe</span>
            <span className="text-3xs font-medium text-muted-foreground">Quầy bán hàng</span>
          </div>
        </Link>

        {/* Desktop Navigation Tabs */}
        <nav className="hidden lg:flex items-center gap-1">
          {navItems.map((item) => {
            const Icon = item.icon;
            const isActive = item.to === "/" ? currentPath === "/" : currentPath.startsWith(item.to);
            return (
              <Link
                key={item.to}
                to={item.to}
                className={`flex min-h-[40px] items-center gap-2 rounded-xl px-3 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? "bg-primary text-primary-foreground shadow-xs font-semibold"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                }`}
              >
                <Icon className="h-4 w-4 shrink-0" />
                <span>{item.label}</span>
              </Link>
            );
          })}
        </nav>
      </div>

      {/* Tablet / Compact Drawer Navigation */}
      <Sheet open={navOpen} onOpenChange={setNavOpen}>
        <SheetContent side="left" className="w-72 p-0">
          <SheetHeader className="border-b border-border px-5 py-4">
            <SheetTitle className="flex items-center gap-2.5 font-bold">
              <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                <Coffee className="h-4 w-4" />
              </span>
              <span>POS Cafe</span>
            </SheetTitle>
          </SheetHeader>
          <nav className="flex flex-col gap-1.5 p-3">
            {navItems.map((item) => {
              const Icon = item.icon;
              const isActive = item.to === "/" ? currentPath === "/" : currentPath.startsWith(item.to);
              return (
                <Link
                  key={item.to}
                  to={item.to}
                  onClick={() => setNavOpen(false)}
                  className={`flex min-h-[48px] items-center gap-3 rounded-xl px-4 text-sm font-semibold transition-colors ${
                    isActive
                      ? "bg-primary text-primary-foreground shadow-xs"
                      : "text-foreground hover:bg-muted"
                  }`}
                >
                  <Icon className="h-5 w-5 shrink-0" />
                  <span>{item.label}</span>
                </Link>
              );
            })}
          </nav>
        </SheetContent>
      </Sheet>

      {/* Right Shell Controls */}
      <div className="flex items-center gap-2.5 sm:gap-3">
        {/* Sound Feedback Toggle */}
        <button
          type="button"
          onClick={toggleSound}
          className={`flex h-11 w-11 min-h-[44px] min-w-[44px] cursor-pointer items-center justify-center rounded-xl border transition select-none active:scale-95 focus:outline-none focus:ring-2 ${
            soundEnabled
              ? "border-emerald-200 bg-emerald-50/70 text-emerald-700 hover:bg-emerald-100/70 focus:ring-emerald-500/20"
              : "border-border bg-muted text-muted-foreground hover:bg-muted/80 focus:ring-muted"
          }`}
          title={soundEnabled ? "Âm thanh phản hồi: Đang bật (Click để tắt)" : "Âm thanh phản hồi: Đang tắt (Click để bật)"}
          aria-label="Bật/tắt âm thanh"
          aria-pressed={soundEnabled}
        >
          {soundEnabled ? <Volume2 className="size-4" /> : <VolumeX className="size-4" />}
        </button>

        {/* Real-time Digital Clock */}
        <div className="hidden sm:flex items-center gap-2 rounded-lg bg-muted px-3 py-1.5">
          <Clock className="h-4 w-4 text-muted-foreground" />
          <span className="text-sm font-semibold font-mono tracking-wide text-foreground">{time}</span>
        </div>

        {/* Staff identity & session actions */}
        <div className="flex items-center gap-2">
          <div className="text-right">
            <div className="text-sm font-semibold text-foreground">{displayName ?? "—"}</div>
            <div className="text-2xs text-muted-foreground">
              {workspace ? WORKSPACE_LABELS[workspace] : "Chưa chọn khu vực"}
            </div>
          </div>
          <Button
            variant="outline"
            size="icon"
            className="h-12 w-12 rounded-xl"
            aria-label="Khóa màn hình"
            disabled={lock.isPending}
            onClick={() => {
              playTapChirp();
              lock.mutate();
            }}
          >
            <Lock className="size-5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-12 w-12 rounded-xl"
            aria-label="Đăng xuất"
            disabled={signOut.isPending}
            onClick={() => {
              playTapChirp();
              signOut.mutate();
            }}
          >
            <LogOut className="size-5" />
          </Button>
        </div>
      </div>
    </header>
  );
}
