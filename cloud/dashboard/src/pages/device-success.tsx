import { motion } from 'framer-motion';
import { CheckCircle2, Zap } from 'lucide-react';

export function DeviceSuccessPage() {
  return (
    <div className="min-h-screen bg-surface-0 flex items-center justify-center">
      <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} className="w-full max-w-sm">
        <div className="rounded-2xl border border-white/[0.06] bg-surface-1/80 backdrop-blur-xl p-8 shadow-2xl shadow-black/40 text-center">
          <div className="flex items-center gap-2.5 justify-center mb-6">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-accent/10 border border-accent/20">
              <Zap className="h-5 w-5 text-accent" />
            </div>
          </div>
          <CheckCircle2 className="h-12 w-12 text-accent mx-auto mb-4" />
          <h1 className="font-display text-xl font-semibold text-white mb-2">CLI authorized</h1>
          <p className="text-sm text-white/40">You can close this tab and return to your terminal.</p>
        </div>
      </motion.div>
    </div>
  );
}
