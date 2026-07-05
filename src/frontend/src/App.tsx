import { AppLayout } from '@/components/AppLayout'
import { TooltipProvider } from '@/components/ui/tooltip'

export default function App() {
  return (
    <TooltipProvider>
      <AppLayout />
    </TooltipProvider>
  )
}
