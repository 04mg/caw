import * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { cn } from '@/features/shared/utils/utils'


const Dialog = DialogPrimitive.Root
const DialogTrigger = DialogPrimitive.Trigger
const DialogPortal = DialogPrimitive.Portal
const DialogClose = DialogPrimitive.Close

const DialogOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn(
      'fixed inset-0 z-50 bg-black/70 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
      className,
    )}
    {...props}
  />
))
DialogOverlay.displayName = DialogPrimitive.Overlay.displayName

// Whether a Radix floating portal (dropdown/select/picker popper wrapper)
// was open during the most recent pointerdown. Radix defers a modal dialog's
// outside-dismissal until the following click, but a dropdown opened above
// the dialog dismisses on the pointerdown itself — so by the time the
// dialog's deferred dispatch runs, the picker portal is already unmounted
// and a DOM query would miss it. Without this snapshot, clicking anywhere
// (the dialog surface lands on the overlay because the open picker disables
// its pointer events) to close an open picker also dismissed the dialog.
let popperOpenAtPointerDown = false
let lastPointerDown: Event | null = null
if (typeof document !== 'undefined') {
  document.addEventListener(
    'pointerdown',
    (event) => {
      lastPointerDown = event
      popperOpenAtPointerDown = !!document.querySelector('[data-radix-popper-content-wrapper]')
    },
    true,
  )
}

const DialogContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & { hideClose?: boolean }
>(({ className, children, hideClose, onPointerDownOutside, onFocusOutside, ...props }, ref) => {
  // Radix renders dropdown/select/picker content into body-level
  // [data-radix-popper-content-wrapper] portals; interacting with them must
  // never dismiss the dialog. Radix dispatches pointerDownOutside /
  // focusOutside as CustomEvents on the interaction target and exposes the
  // native event via event.detail.originalEvent — prefer its target, falling
  // back to event.target.
  const isPortalInteraction = (target: EventTarget | null): boolean =>
    !!((target as HTMLElement | null)?.closest?.('[data-radix-popper-content-wrapper]'))

  // True when the dismissal stems from a pointerdown that happened while a
  // picker portal was open (its deferred dispatch fires after the picker has
  // closed), or from an interaction inside such a portal.
  const shouldKeepDialogOpen = (event: {
    detail?: { originalEvent?: Event }
    target: EventTarget | null
  }): boolean => {
    const original = event.detail?.originalEvent
    if (isPortalInteraction(original?.target ?? event.target)) return true
    return original != null && original === lastPointerDown && popperOpenAtPointerDown
  }

  const handlePointerDownOutside = (
    event: Parameters<NonNullable<React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>['onPointerDownOutside']>>[0],
  ) => {
    if (shouldKeepDialogOpen(event)) {
      event.preventDefault()
      return
    }
    onPointerDownOutside?.(event)
  }

  const handleFocusOutside = (
    event: Parameters<NonNullable<React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>['onFocusOutside']>>[0],
  ) => {
    if (shouldKeepDialogOpen(event)) {
      event.preventDefault()
      return
    }
    onFocusOutside?.(event)
  }

  return (
    <DialogPortal>
      <DialogOverlay />
      <DialogPrimitive.Content
        ref={ref}
        className={cn(
          'fixed left-[50%] top-[50%] z-50 flex max-h-[85vh] w-full max-w-lg translate-x-[-50%] translate-y-[-50%] flex-col overflow-x-hidden overflow-y-auto border border-border bg-background p-4 shadow-lg duration-200 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 sm:rounded-lg',
          className,
        )}
        onPointerDownOutside={handlePointerDownOutside}
        onFocusOutside={handleFocusOutside}
        {...props}
      >
      {children}
      {!hideClose && (
        <DialogPrimitive.Close className="absolute right-3 top-3 rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:pointer-events-none">
          <X className="h-4 w-4" />
          <span className="sr-only">Close</span>
        </DialogPrimitive.Close>
      )}
      </DialogPrimitive.Content>
    </DialogPortal>
  )
})
DialogContent.displayName = DialogPrimitive.Content.displayName

function DialogHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('flex flex-col space-y-1.5 text-center sm:text-left', className)}
      {...props}
    />
  )
}

function DialogTitle({
  className,
  ...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      className={cn('text-sm font-medium leading-none tracking-tight', className)}
      {...props}
    />
  )
}

function DialogDescription({
  className,
  ...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      className={cn('text-xs text-muted-foreground', className)}
      {...props}
    />
  )
}

export {
  Dialog,
  DialogPortal,
  DialogOverlay,
  DialogTrigger,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
}