import { motion } from 'framer-motion';

interface PageWrapperProps {
  title: string;
  description?: string;
  children: React.ReactNode;
  action?: React.ReactNode;
}

export function PageWrapper({ title, description, children, action }: PageWrapperProps) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, ease: [0.25, 0.46, 0.45, 0.94] }}
      className="min-h-screen"
    >
      <div className="flex items-start justify-between mb-8">
        <div>
          <h1 className="font-display text-2xl font-bold tracking-tight text-white">{title}</h1>
          {description && (
            <p className="mt-1 text-sm text-white/40">{description}</p>
          )}
        </div>
        {action}
      </div>
      {children}
    </motion.div>
  );
}
