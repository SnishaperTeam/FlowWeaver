import React, { useCallback, useEffect, useMemo, useState, useContext } from 'react';
import {
  Box, Typography, Button, TextField, IconButton, Grid, Chip, Divider, CircularProgress,
  InputAdornment,
} from '@mui/material';
import { alpha } from '@mui/material/styles';
import {
  AddCircle, Delete, RefreshCw, Download, CheckCircle, Search, Share, Lock,
} from '../lib/icons';
import {
  GetSubscriptions, AddSubscription, UpdateSubscription, DeleteSubscription,
  ActivateSubscription, SelectSubscriptionNode, TestSubscriptionNode,
} from '../api/bindings';
import ProxyNodeRow, { ProxyNode, ProxyGroup } from '../components/ProxyNodeRow';
import { useTranslation } from '../i18n/I18nContext';
import { toast } from '../lib/toast';
import { SettingsCtx } from '../App';

interface Subscription {
  id: string;
  name: string;
  url: string;
  builtin: boolean;
  active: boolean;
  updated_at: number;
  upload: number;
  download: number;
  total: number;
  left: number;
  expire_time: number;
  node_count: number;
  rule_count: number;
  groups: ProxyGroup[];
  nodes: ProxyNode[];
  selection: Record<string, string>;
  error: string;
}

const formatBytes = (bytes: number): string => {
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 2)} ${units[i]}`;
};

const formatDate = (unix: number): string => {
  if (!unix || unix <= 0) return '';
  const d = new Date(unix * 1000);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
};

const Subscription: React.FC = () => {
  const { t } = useTranslation();
  const { updateCache } = useContext(SettingsCtx);
  const [subs, setSubs] = useState<Subscription[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState('');

  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [adding, setAdding] = useState(false);
  const [showAdd, setShowAdd] = useState(false);

  const [activeId, setActiveId] = useState('');
  const [activeGroup, setActiveGroup] = useState('');
  const [search, setSearch] = useState('');
  const [delays, setDelays] = useState<Record<string, number>>({});
  const [testingKey, setTestingKey] = useState('');

  const loadData = useCallback(async () => {
    try {
      const list = (await GetSubscriptions()) as Subscription[];
      setSubs(list || []);
    } catch (err: any) {
      toast.error(err?.message || String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadData();
  }, [loadData]);

  const activeSub = useMemo(
    () => subs.find((s) => s.id === activeId) || null,
    [subs, activeId]
  );

  useEffect(() => {
    if (!activeSub) {
      setActiveGroup('');
      return;
    }
    if (activeSub.builtin || activeSub.groups.length === 0) {
      setActiveGroup('');
      return;
    }
    const stillValid = activeSub.groups.some((g) => g.name === activeGroup);
    if (!stillValid) {
      setActiveGroup(activeSub.groups[0].name);
    }
  }, [activeSub, activeGroup]);

  const handleAdd = async () => {
    if (!url.trim()) {
      toast.error(t('subscriptions.url_required'));
      return;
    }
    setAdding(true);
    try {
      await AddSubscription(name.trim(), url.trim());
      toast.success(t('subscriptions.add_success'));
      setName('');
      setUrl('');
      setShowAdd(false);
      await loadData();
    } catch (err: any) {
      toast.error(err?.message || String(err));
    } finally {
      setAdding(false);
    }
  };

  const handleUpdate = async (id: string) => {
    setBusyId(id);
    try {
      await UpdateSubscription(id);
      toast.success(t('subscriptions.update_success'));
      await loadData();
    } catch (err: any) {
      toast.error(err?.message || String(err));
    } finally {
      setBusyId('');
    }
  };

  const handleDelete = async (sub: Subscription) => {
    if (!confirm(t('subscriptions.delete_confirm', { name: sub.name }))) return;
    setBusyId(sub.id);
    try {
      await DeleteSubscription(sub.id);
      toast.success(t('common.success'));
      if (activeId === sub.id) setActiveId('');
      await loadData();
    } catch (err: any) {
      toast.error(err?.message || String(err));
    } finally {
      setBusyId('');
    }
  };

  const handleActivate = async (sub: Subscription) => {
    setBusyId(sub.id);
    try {
      await ActivateSubscription(sub.id);
      toast.success(t('subscriptions.activate_success', { name: sub.name }));
      setActiveId(sub.id);
      await loadData();
      updateCache({ rulesVersion: Date.now() });
    } catch (err: any) {
      toast.error(err?.message || String(err));
    } finally {
      setBusyId('');
    }
  };

  const handleSelectNode = async (nodeName: string) => {
    if (!activeSub || !activeGroup) return;
    try {
      await SelectSubscriptionNode(activeSub.id, activeGroup, nodeName);
      setSubs((prev) =>
        prev.map((s) =>
          s.id === activeSub.id
            ? { ...s, selection: { ...s.selection, [activeGroup]: nodeName } }
            : s
        )
      );
      toast.success(t('subscriptions.node_selected', { name: nodeName }));
    } catch (err: any) {
      toast.error(err?.message || String(err));
    }
  };

  const handleTest = async (node: ProxyNode) => {
    const key = node.name;
    setTestingKey(key);
    setDelays((prev) => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
    try {
      const ms = (await TestSubscriptionNode(node.server, node.port)) as number;
      setDelays((prev) => ({ ...prev, [key]: ms }));
    } catch (err: any) {
      setDelays((prev) => ({ ...prev, [key]: -1 }));
      toast.error(err?.message || String(err));
    } finally {
      setTestingKey('');
    }
  };

  const visibleNodes = useMemo(() => {
    if (!activeSub || activeSub.nodes.length === 0) return [];
    const group = activeSub.groups.find((g) => g.name === activeGroup);
    const members = group ? group.members : [];
    const byName = new Map(activeSub.nodes.map((n) => [n.name, n]));
    const ordered = members
      .map((m) => byName.get(m))
      .filter((n): n is ProxyNode => Boolean(n));
    const rest = activeSub.nodes.filter((n) => !members.includes(n.name));
    const all = group ? [...ordered, ...rest] : activeSub.nodes;

    const q = search.trim().toLowerCase();
    if (!q) return all;
    return all.filter(
      (n) => n.name.toLowerCase().includes(q) || n.server.toLowerCase().includes(q)
    );
  }, [activeSub, activeGroup, search]);

  return (
    <Box sx={{ flexGrow: 1, minHeight: 0, width: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <Box sx={{ pt: 4, flexShrink: 0, display: 'flex', flexWrap: 'wrap', justifyContent: 'space-between', alignItems: { xs: 'flex-start', sm: 'flex-end' }, gap: 2, mb: 4 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
          <Box sx={{ p: 1.25, borderRadius: 1.5, border: 1, color: 'primary.main', bgcolor: (theme) => alpha(theme.palette.primary.main, 0.1), borderColor: (theme) => alpha(theme.palette.primary.main, 0.1), display: 'flex' }}>
            <Share size={20} />
          </Box>
          <Typography variant="h5" sx={{ fontWeight: 700, letterSpacing: '-0.02em' }}>{t('subscriptions.title')}</Typography>
        </Box>
        <Button onClick={() => setShowAdd((v) => !v)} variant="outlined" size="small" startIcon={<AddCircle size={14} />}>
          {t('subscriptions.add')}
        </Button>
      </Box>

      <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto', overflowX: 'hidden', pb: 6, display: 'flex', flexDirection: 'column', gap: 4 }}>
        {showAdd && (
          <Box sx={{ p: 2.5, bgcolor: 'background.paper', border: 1, borderColor: 'divider', borderRadius: 2, boxShadow: 1, display: 'flex', flexDirection: 'column', gap: 2 }}>
            <Typography variant="caption" sx={{ fontSize: 11, color: 'text.secondary' }}>{t('subscriptions.add_hint')}</Typography>
            <Box sx={{ display: 'flex', gap: 1.5, flexWrap: 'wrap' }}>
              <TextField
                size="small"
                label={t('subscriptions.name')}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t('subscriptions.name_placeholder')}
                sx={{ flex: '1 1 14rem', '& input': { fontSize: '0.8rem' } }}
              />
              <TextField
                data-tut="sub-input"
                size="small"
                label={t('subscriptions.url')}
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://"
                sx={{ flex: '2 1 22rem', '& input': { fontSize: '0.8rem' } }}
              />
              <Button onClick={handleAdd} disabled={adding} variant="contained" size="small" startIcon={adding ? <CircularProgress size={12} /> : <Download size={14} />} sx={{ flexShrink: 0 }}>
                {adding ? t('subscriptions.importing') : t('subscriptions.import')}
              </Button>
            </Box>
          </Box>
        )}

        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 0.5, color: 'text.secondary' }}>
            <Share size={18} aria-hidden />
            <Typography variant="body2" sx={{ fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
              {t('subscriptions.my_subscriptions')}
            </Typography>
          </Box>

          {loading ? (
            <Box sx={{ py: 8, display: 'flex', justifyContent: 'center' }}>
              <CircularProgress size={22} />
            </Box>
          ) : subs.length === 0 ? (
            <Box sx={{ py: 10, display: 'flex', flexDirection: 'column', alignItems: 'center', color: 'text.secondary', opacity: 0.7, bgcolor: 'background.paper', border: '1px dashed', borderColor: 'divider', borderRadius: 2 }}>
              <Lock size={30} strokeWidth={1.5} />
              <Typography variant="body2" sx={{ mt: 1.5 }}>{t('subscriptions.no_subscriptions')}</Typography>
            </Box>
          ) : (
            <Grid container spacing={2}>
              {subs.map((s) => {
                const sUsed = s.upload + s.download;
                const sPct = s.total > 0 ? Math.min(100, Math.round((sUsed / s.total) * 100)) : 0;
                return (
                  <Grid key={s.id} size={{ xs: 12, md: 6, xl: 4 }}>
                    <Box
                      onClick={() => { if (!s.builtin) setActiveId(s.id); }}
                      sx={{
                        p: 2.5, bgcolor: 'background.paper', border: 1,
                        borderColor: s.active ? 'primary.main' : 'divider',
                        borderRadius: 2, boxShadow: 1, transition: 'all 0.2s',
                        cursor: s.builtin ? 'default' : 'pointer',
                        '&:hover': s.builtin ? {} : { boxShadow: 3, borderColor: 'primary.main' },
                      }}
                    >
                      <Box sx={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 1.5 }}>
                        <Box sx={{ minWidth: 0 }}>
                          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <Typography variant="body2" sx={{ fontWeight: 700, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                              {s.name}
                            </Typography>
                            {s.active && <CheckCircle size={14} aria-hidden style={{ color: 'var(--mui-palette-primary-main)', flexShrink: 0 }} />}
                          </Box>
                          <Typography variant="caption" sx={{ fontSize: 10, color: 'text.secondary', fontWeight: 700, display: 'block', mt: 0.25 }}>
                            {s.builtin
                              ? t('subscriptions.builtin_badge')
                              : t('subscriptions.node_count', { count: s.node_count })}
                          </Typography>
                        </Box>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
                          {!s.builtin && (
                            <>
                              <IconButton
                                size="small"
                                aria-label={t('subscriptions.update_aria', { name: s.name })}
                                disabled={busyId === s.id}
                                onClick={(e) => { e.stopPropagation(); handleUpdate(s.id); }}
                                sx={{ color: 'text.secondary', '&:hover': { color: 'primary.main' } }}
                              >
                                <RefreshCw size={15} />
                              </IconButton>
                              <IconButton
                                size="small"
                                aria-label={t('subscriptions.delete_aria', { name: s.name })}
                                disabled={busyId === s.id}
                                onClick={(e) => { e.stopPropagation(); handleDelete(s); }}
                                sx={{ color: 'text.secondary', '&:hover': { color: 'error.main' } }}
                              >
                                <Delete size={15} />
                              </IconButton>
                            </>
                          )}
                        </Box>
                      </Box>

                      {s.error && (
                        <Typography variant="caption" sx={{ display: 'block', mt: 1, fontSize: 10, fontWeight: 700, color: 'error.main' }}>
                          {s.error}
                        </Typography>
                      )}

                      {s.total > 0 && (
                        <Box sx={{ mt: 2 }}>
                          <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 0.75 }}>
                            <Typography variant="caption" sx={{ fontSize: 10, fontWeight: 700, color: 'text.secondary' }}>
                              {t('subscriptions.remaining')}
                            </Typography>
                            <Typography variant="caption" sx={{ fontSize: 10, fontWeight: 700 }}>
                              {formatBytes(s.left < 0 ? 0 : s.left)} / {formatBytes(s.total)}
                            </Typography>
                          </Box>
                          <Box sx={{ height: 5, borderRadius: 3, bgcolor: 'action.hover', overflow: 'hidden' }}>
                            <Box sx={{
                              height: '100%', borderRadius: 3,
                              width: `${sPct}%`,
                              bgcolor: sPct > 90 ? 'error.main' : sPct > 70 ? 'warning.main' : 'primary.main',
                              transition: 'width 0.3s',
                            }} />
                          </Box>
                          <Typography variant="caption" sx={{ display: 'block', mt: 0.75, fontSize: 10, color: 'text.secondary', fontWeight: 700 }}>
                            {t('subscriptions.used', { used: formatBytes(sUsed) })}
                          </Typography>
                        </Box>
                      )}

                      {s.expire_time > 0 && (
                        <Box sx={{ mt: 1.5 }}>
                          <Chip
                            size="small"
                            label={t('subscriptions.expires', { date: formatDate(s.expire_time) })}
                            sx={{ fontSize: 10, fontWeight: 700, height: 20 }}
                          />
                        </Box>
                      )}

                      {!s.builtin && !s.active && (
                        <Button
                          fullWidth
                          size="small"
                          variant="outlined"
                          disabled={busyId === s.id || s.node_count === 0}
                          onClick={(e) => { e.stopPropagation(); handleActivate(s); }}
                          sx={{ mt: 2, fontSize: 11, fontWeight: 700 }}
                        >
                          {t('subscriptions.activate')}
                        </Button>
                      )}
                    </Box>
                  </Grid>
                );
              })}
            </Grid>
          )}
        </Box>

        {activeSub && !activeSub.builtin && (
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <Divider />
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', px: 0.5, flexWrap: 'wrap', gap: 1 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, color: 'text.secondary' }}>
                <Share size={18} aria-hidden />
                <Typography variant="body2" sx={{ fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                  {t('subscriptions.node_selection')}
                </Typography>
              </Box>
              <TextField
                size="small"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={t('subscriptions.search_placeholder')}
                slotProps={{ input: { startAdornment: <InputAdornment position="start"><Search size={14} /></InputAdornment> } }}
                sx={{ width: 220, '& input': { fontSize: '0.78rem' } }}
              />
            </Box>

            {activeSub.groups.length > 1 && (
              <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                {activeSub.groups.map((g) => (
                  <Button
                    key={g.name}
                    size="small"
                    variant={g.name === activeGroup ? 'contained' : 'outlined'}
                    onClick={() => setActiveGroup(g.name)}
                    sx={{ fontSize: 11, fontWeight: 700 }}
                  >
                    {g.name}
                  </Button>
                ))}
              </Box>
            )}

            {activeSub.groups.length > 0 && (
              <Box sx={{ px: 0.5 }}>
                <Typography variant="caption" sx={{ fontSize: 11, fontWeight: 700, color: 'text.secondary' }}>
                  {t('subscriptions.current_selection', { name: activeSub.selection[activeGroup] || t('subscriptions.not_selected') })}
                </Typography>
              </Box>
            )}

            {visibleNodes.length === 0 ? (
              <Box sx={{ py: 8, display: 'flex', flexDirection: 'column', alignItems: 'center', color: 'text.secondary', opacity: 0.7, bgcolor: 'background.paper', border: '1px dashed', borderColor: 'divider', borderRadius: 2 }}>
                <Share size={28} strokeWidth={1.5} />
                <Typography variant="body2" sx={{ mt: 1.5 }}>{t('subscriptions.no_nodes')}</Typography>
              </Box>
            ) : (
              <Box>
                {visibleNodes.map((n) => (
                  <ProxyNodeRow
                    key={n.name}
                    node={n}
                    selected={activeSub.selection[activeGroup] === n.name}
                    delay={delays[n.name]}
                    testing={testingKey === n.name}
                    onSelect={() => handleSelectNode(n.name)}
                    onTest={() => handleTest(n)}
                  />
                ))}
              </Box>
            )}
          </Box>
        )}

        {activeSub && activeSub.builtin && (
          <Box sx={{ p: 2.5, display: 'flex', alignItems: 'center', gap: 1.5, bgcolor: 'background.paper', border: 1, borderColor: 'divider', borderRadius: 2 }}>
            <CheckCircle size={18} aria-hidden style={{ color: 'var(--mui-palette-success-main)', flexShrink: 0 }} />
            <Box>
              <Typography variant="body2" sx={{ fontWeight: 700 }}>{t('subscriptions.default_rules_active')}</Typography>
              <Typography variant="caption" sx={{ fontSize: 11, color: 'text.secondary' }}>
                {t('subscriptions.default_rules_desc')}
              </Typography>
            </Box>
          </Box>
        )}
      </Box>

    </Box>
  );
};

export default Subscription;
