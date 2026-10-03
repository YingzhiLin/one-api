import React, { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Form,
  Label,
  Popup,
  Pagination,
  Table,
} from 'semantic-ui-react';
import { Link } from 'react-router-dom';
import {
  API,
  copy,
  showError,
  showInfo,
  showSuccess,
  showWarning,
  timestamp2string,
} from '../helpers';

import { ITEMS_PER_PAGE } from '../constants';
import { renderQuota } from '../helpers/render';

function renderTimestamp(timestamp) {
  return <>{timestamp2string(timestamp)}</>;
}

function renderStatus(status, t) {
  switch (status) {
    case 1:
      return (
        <Label basic color='green'>
          {t('redemption.status.unused')}
        </Label>
      );
    case 2:
      return (
        <Label basic color='red'>
          {t('redemption.status.disabled')}
        </Label>
      );
    case 3:
      return (
        <Label basic color='grey'>
          {t('redemption.status.used')}
        </Label>
      );
    default:
      return (
        <Label basic color='black'>
          {t('redemption.status.unknown')}
        </Label>
      );
  }
}

const RedemptionsTable = () => {
  const { t } = useTranslation();
  const [redemptions, setRedemptions] = useState([]);
  const [loading, setLoading] = useState(true);
  const [activePage, setActivePage] = useState(1);
  const [searchKeyword, setSearchKeyword] = useState('');
  const [searching, setSearching] = useState(false);
  const [totalRedemptions, setTotalRedemptions] = useState(0);
  const [searchResults, setSearchResults] = useState(null);
  const [submittedKeyword, setSubmittedKeyword] = useState('');
  const requestId = useRef(0);

  const loadRedemptions = async (startIdx) => {
    const currentRequest = ++requestId.current;
    setLoading(true);
    setSearching(false);
    try {
      let res = await API.get(`/api/redemption/?p=${startIdx}`);
      if (currentRequest !== requestId.current) return;
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      const lastPageIndex = Math.max(0, Math.ceil(res.data.total / ITEMS_PER_PAGE) - 1);
      const pageIndex = Math.min(startIdx, lastPageIndex);
      if (pageIndex !== startIdx) {
        res = await API.get(`/api/redemption/?p=${pageIndex}`);
        if (currentRequest !== requestId.current) return;
      }
      const { success, message, data, total } = res.data;
      if (success) {
        setRedemptions(data);
        setTotalRedemptions(total);
        setSearchResults(null);
        setSubmittedKeyword('');
        setActivePage(pageIndex + 1);
      } else {
        showError(message);
      }
    } catch (error) {
      if (currentRequest === requestId.current) showError(error);
    } finally {
      if (currentRequest === requestId.current) setLoading(false);
    }
  };

  const onPaginationChange = async (e, { activePage: targetPage }) => {
    if (loading || searching) return;
    if (searchResults !== null) {
      setRedemptions(searchResults.slice((targetPage - 1) * ITEMS_PER_PAGE, targetPage * ITEMS_PER_PAGE));
      setActivePage(targetPage);
    } else {
      await loadRedemptions(targetPage - 1);
    }
  };

  useEffect(() => {
    loadRedemptions(0)
      .then()
      .catch((reason) => {
        showError(reason);
      });
  }, []);

  const manageRedemption = async (id, action) => {
    let data = { id };
    let res;
    switch (action) {
      case 'delete':
        res = await API.delete(`/api/redemption/${id}/`);
        break;
      case 'enable':
        data.status = 1;
        res = await API.put('/api/redemption/?status_only=true', data);
        break;
      case 'disable':
        data.status = 2;
        res = await API.put('/api/redemption/?status_only=true', data);
        break;
    }
    const { success, message } = res.data;
    if (success) {
      showSuccess(t('token.messages.operation_success'));
      if (action === 'delete') {
        if (searchResults !== null) {
          await loadSearchRedemptions(submittedKeyword, activePage);
        } else {
          await loadRedemptions(activePage - 1);
        }
      } else {
        const updateRedemption = (existing) => existing.id === id
          ? { ...existing, status: res.data.data.status }
          : existing;
        setRedemptions((previous) => previous.map(updateRedemption));
        if (searchResults !== null) {
          setSearchResults((previous) => previous.map(updateRedemption));
        }
      }
    } else {
      showError(message);
    }
  };

  const loadSearchRedemptions = async (keyword, targetPage = 1) => {
    const currentRequest = ++requestId.current;
    setSearching(true);
    setLoading(true);
    try {
      const res = await API.get(`/api/redemption/search?keyword=${encodeURIComponent(keyword)}`);
      if (currentRequest !== requestId.current) return;
      const { success, message, data } = res.data;
      if (success) {
        const page = Math.min(targetPage, Math.max(1, Math.ceil(data.length / ITEMS_PER_PAGE)));
        setSearchResults(data);
        setSubmittedKeyword(keyword);
        setTotalRedemptions(data.length);
        setRedemptions(data.slice((page - 1) * ITEMS_PER_PAGE, page * ITEMS_PER_PAGE));
        setActivePage(page);
      } else {
        showError(message);
      }
    } catch (error) {
      if (currentRequest === requestId.current) showError(error);
    } finally {
      if (currentRequest === requestId.current) {
        setSearching(false);
        setLoading(false);
      }
    }
  };

  const searchRedemptions = async () => {
    if (searchKeyword === '') {
      await loadRedemptions(0);
    } else {
      await loadSearchRedemptions(searchKeyword);
    }
  };

  const handleKeywordChange = async (e, { value }) => {
    setSearchKeyword(value.trim());
  };

  const sortRedemption = (key) => {
    if (redemptions.length === 0) return;
    setLoading(true);
    let sortedRedemptions = [...(searchResults !== null ? searchResults : redemptions)];
    sortedRedemptions.sort((a, b) => {
      if (!isNaN(a[key])) {
        // If the value is numeric, subtract to sort
        return a[key] - b[key];
      } else {
        // If the value is not numeric, sort as strings
        return ('' + a[key]).localeCompare(b[key]);
      }
    });
    if (sortedRedemptions[0].id === (searchResults !== null ? searchResults[0].id : redemptions[0].id)) {
      sortedRedemptions.reverse();
    }
    if (searchResults !== null) {
      setSearchResults(sortedRedemptions);
      setRedemptions(sortedRedemptions.slice((activePage - 1) * ITEMS_PER_PAGE, activePage * ITEMS_PER_PAGE));
    } else {
      setRedemptions(sortedRedemptions);
    }
    setLoading(false);
  };

  const refresh = async () => {
    if (searchResults !== null) {
      await loadSearchRedemptions(submittedKeyword, activePage);
    } else {
      await loadRedemptions(activePage - 1);
    }
  };

  return (
    <>
      <Form onSubmit={searchRedemptions}>
        <Form.Input
          icon='search'
          fluid
          iconPosition='left'
          placeholder={t('redemption.search')}
          value={searchKeyword}
          loading={searching}
          onChange={handleKeywordChange}
        />
      </Form>

      <Table basic={'very'} compact size='small'>
        <Table.Header>
          <Table.Row>
            <Table.HeaderCell
              style={{ cursor: 'pointer' }}
              onClick={() => {
                sortRedemption('id');
              }}
            >
              {t('redemption.table.id')}
            </Table.HeaderCell>
            <Table.HeaderCell
              style={{ cursor: 'pointer' }}
              onClick={() => {
                sortRedemption('name');
              }}
            >
              {t('redemption.table.name')}
            </Table.HeaderCell>
            <Table.HeaderCell
              style={{ cursor: 'pointer' }}
              onClick={() => {
                sortRedemption('status');
              }}
            >
              {t('redemption.table.status')}
            </Table.HeaderCell>
            <Table.HeaderCell
              style={{ cursor: 'pointer' }}
              onClick={() => {
                sortRedemption('quota');
              }}
            >
              {t('redemption.table.quota')}
            </Table.HeaderCell>
            <Table.HeaderCell
              style={{ cursor: 'pointer' }}
              onClick={() => {
                sortRedemption('created_time');
              }}
            >
              {t('redemption.table.created_time')}
            </Table.HeaderCell>
            <Table.HeaderCell
              style={{ cursor: 'pointer' }}
              onClick={() => {
                sortRedemption('redeemed_time');
              }}
            >
              {t('redemption.table.redeemed_time')}
            </Table.HeaderCell>
            <Table.HeaderCell>{t('redemption.table.actions')}</Table.HeaderCell>
          </Table.Row>
        </Table.Header>

        <Table.Body>
          {redemptions.map((redemption) => {
              if (redemption.deleted) return <></>;
              return (
                <Table.Row key={redemption.id}>
                  <Table.Cell>{redemption.id}</Table.Cell>
                  <Table.Cell>
                    {redemption.name ? redemption.name : t('redemption.table.no_name')}
                  </Table.Cell>
                  <Table.Cell>{renderStatus(redemption.status, t)}</Table.Cell>
                  <Table.Cell>{renderQuota(redemption.quota, t)}</Table.Cell>
                  <Table.Cell>
                    {renderTimestamp(redemption.created_time)}
                  </Table.Cell>
                  <Table.Cell>
                    {redemption.redeemed_time
                      ? renderTimestamp(redemption.redeemed_time)
                      : t('redemption.table.not_redeemed')}{' '}
                  </Table.Cell>
                  <Table.Cell>
                    <div>
                      <Button
                        size={'tiny'}
                        positive
                        onClick={async () => {
                          if (await copy(redemption.key)) {
                            showSuccess(t('token.messages.copy_success'));
                          } else {
                            showWarning(t('token.messages.copy_failed'));
                            setSearchKeyword(redemption.key);
                          }
                        }}
                      >
                        {t('redemption.buttons.copy')}
                      </Button>
                      <Popup
                        trigger={
                          <Button size='tiny' negative>
                            {t('redemption.buttons.delete')}
                          </Button>
                        }
                        on='click'
                        flowing
                        hoverable
                      >
                        <Button
                          negative
                          onClick={() => {
                            manageRedemption(redemption.id, 'delete');
                          }}
                        >
                          {t('redemption.buttons.confirm_delete')}
                        </Button>
                      </Popup>
                      <Button
                        size={'tiny'}
                        disabled={redemption.status === 3} // used
                        onClick={() => {
                          manageRedemption(
                            redemption.id,
                            redemption.status === 1 ? 'disable' : 'enable'
                          );
                        }}
                      >
                        {redemption.status === 1
                          ? t('redemption.buttons.disable')
                          : t('redemption.buttons.enable')}
                      </Button>
                      <Button
                        size={'tiny'}
                        as={Link}
                        to={'/redemption/edit/' + redemption.id}
                      >
                        {t('redemption.buttons.edit')}
                      </Button>
                    </div>
                  </Table.Cell>
                </Table.Row>
              );
            })}
        </Table.Body>

        <Table.Footer>
          <Table.Row>
            <Table.HeaderCell colSpan='7'>
              <Button
                size='small'
                as={Link}
                to='/redemption/add'
                loading={loading}
              >
                {t('redemption.buttons.add')}
              </Button>
              <Button size='small' onClick={refresh} loading={loading}>
                {t('redemption.buttons.refresh')}
              </Button>
              <Pagination
                floated='right'
                activePage={activePage}
                onPageChange={onPaginationChange}
                size='small'
                siblingRange={1}
                disabled={loading || searching}
                totalPages={Math.max(1, Math.ceil(totalRedemptions / ITEMS_PER_PAGE))}
              />
            </Table.HeaderCell>
          </Table.Row>
        </Table.Footer>
      </Table>
    </>
  );
};

export default RedemptionsTable;
