import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';

interface PageMetaContextValue {
  pageTitle: string;
  setPageTitle: (title: string) => void;
}

const PageMetaContext = createContext<PageMetaContextValue | null>(null);

export function PageMetaProvider({ children }: { children: ReactNode }) {
  const [pageTitle, setPageTitle] = useState('');
  return (
    <PageMetaContext.Provider value={{ pageTitle, setPageTitle }}>
      {children}
    </PageMetaContext.Provider>
  );
}

export function usePageMeta(): PageMetaContextValue {
  const ctx = useContext(PageMetaContext);
  if (!ctx) throw new Error('usePageMeta must be used inside <PageMetaProvider>');
  return ctx;
}

/** Set the page title in mount; reset is handled by next page's set. */
export function useSetPageTitle(title: string): void {
  const { setPageTitle } = usePageMeta();
  useEffect(() => {
    setPageTitle(title);
  }, [title, setPageTitle]);
}
