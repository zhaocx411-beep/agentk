import { KsapIdentity } from '@ks-cqc/ksap-identity-sdk';

export type ApUser = {
  username: string;
  displayName: string;
};

export interface ApIdentityVerifier {
  verify(
    rawToken: string | undefined,
    peerIp: string | undefined,
  ): Promise<ApUser>;
}

export interface KsapIdentityClient {
  init(options: {
    jwksUrl: string;
    verifyIss: boolean;
    trustedHosts: string[];
    debug: boolean;
    fileLogConfig: { isOpen: boolean };
  }): Promise<void>;

  verifyToken(token: string, ip: string): {
    result: number;
    message: string;
    data: unknown;
  };
}

function normalizePeerIp(peerIp: string | undefined): string {
  if (!peerIp) {
    throw new Error('Missing AccessProxy peer address');
  }
  return peerIp.startsWith('::ffff:') ? peerIp.slice('::ffff:'.length) : peerIp;
}

export async function openApIdentityVerifier(
  options: {
    jwksUrl: string;
    expectedHost: string;
  },
  client: KsapIdentityClient = new KsapIdentity(),
): Promise<ApIdentityVerifier> {
  const ksapIdentitySdk = client;
  await ksapIdentitySdk.init({
    jwksUrl: options.jwksUrl,
    verifyIss: true,
    trustedHosts: [options.expectedHost],
    debug: false,
    fileLogConfig: { isOpen: false },
  });

  return {
    async verify(rawToken, peerIp) {
      if (!rawToken) {
        throw new Error('Missing AP identity');
      }
      const result = ksapIdentitySdk.verifyToken(
        rawToken,
        normalizePeerIp(peerIp),
      );
      const data = result.data as
        | { username?: unknown; name?: unknown }
        | undefined;
      if (
        result.result !== 1 ||
        typeof data?.username !== 'string' ||
        data.username.length === 0
      ) {
        throw new Error('Invalid AP identity');
      }
      return {
        username: data.username,
        displayName:
          typeof data.name === 'string' && data.name.length > 0
            ? data.name
            : data.username,
      };
    },
  };
}
