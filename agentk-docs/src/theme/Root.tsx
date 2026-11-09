import {useEffect, type ReactNode} from 'react';
import useBaseUrl from '@docusaurus/useBaseUrl';

const POLL_INTERVAL_MS = 60_000;

type CatalogStatus = 'connecting' | 'live' | 'offline';

function setCatalogStatus(status: CatalogStatus) {
  document.documentElement.dataset.capabilityCatalogStatus = status;
}

export default function Root({children}: {children: ReactNode}): ReactNode {
  const capabilityEndpoint = useBaseUrl('/_agentk/capabilities');

  useEffect(() => {
    let disposed = false;
    const controller = new AbortController();

    async function checkCatalog() {
      try {
        const response = await fetch(capabilityEndpoint, {
          method: 'POST',
          headers: {
            Accept: 'application/json',
            'Content-Type': 'application/json',
            'Connect-Protocol-Version': '1',
          },
          body: JSON.stringify({pageSize: 1}),
          signal: controller.signal,
        });
        if (!disposed) {
          setCatalogStatus(response.ok ? 'live' : 'offline');
        }
      } catch {
        if (!disposed) {
          setCatalogStatus('offline');
        }
      }
    }

    setCatalogStatus('connecting');
    void checkCatalog();
    const poll = window.setInterval(() => void checkCatalog(), POLL_INTERVAL_MS);

    return () => {
      disposed = true;
      controller.abort();
      window.clearInterval(poll);
    };
  }, [capabilityEndpoint]);

  return children;
}
