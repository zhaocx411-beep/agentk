import {createContext, useContext, useEffect, type ReactNode} from 'react';

type SetSidebar = (content: ReactNode | null) => void;

const DocRightSidebarContext = createContext<SetSidebar | null>(null);

export function DocRightSidebarProvider({
  children,
  setSidebar,
}: {
  children: ReactNode;
  setSidebar: SetSidebar;
}): ReactNode {
  return (
    <DocRightSidebarContext.Provider value={setSidebar}>
      {children}
    </DocRightSidebarContext.Provider>
  );
}

export function DocRightSidebar({children}: {children: ReactNode}): null {
  const setSidebar = useContext(DocRightSidebarContext);

  useEffect(() => {
    if (!setSidebar) return undefined;
    setSidebar(children);
    return () => setSidebar(null);
  }, [children, setSidebar]);

  return null;
}
