//! Explicit process-only default. Never a fallback from a failed OS vault.
use mnema_private_vault_facade::{Boundary, Error, Namespace, RECORD_LEN, SecretBlob};
use std::{
    collections::HashMap,
    marker::PhantomData,
    rc::Rc,
    sync::{Arc, Mutex},
    thread::{self, ThreadId},
};
use zeroize::Zeroizing;
#[cfg(test)]
#[derive(Clone, Copy, Default)]
pub(crate) enum Fault {
    #[default]
    None,
    Read,
    WriteBefore,
    WriteAfter,
}
#[derive(Default)]
struct State {
    records: HashMap<[u8; 32], Zeroizing<Vec<u8>>>,
    owners: HashMap<[u8; 32], ThreadId>,
    #[cfg(test)]
    fault: Fault,
    #[cfg(test)]
    events: Vec<&'static str>,
}
#[derive(Clone, Default)]
pub(crate) struct Ephemeral(Arc<Mutex<State>>);
pub(crate) struct Guard {
    memory: Ephemeral,
    key: [u8; 32],
    thread: ThreadId,
    _thread_bound: PhantomData<Rc<()>>,
}
impl Drop for Guard {
    fn drop(&mut self) {
        if let Ok(mut state) = self.memory.0.lock() {
            state.owners.remove(&self.key);
        }
    }
}
impl Ephemeral {
    pub(crate) fn clear(&self) -> Result<(), Error> {
        let mut s = self.0.lock().map_err(|_| Error::Poisoned)?;
        if !s.owners.is_empty() {
            return Err(Error::Busy);
        }
        s.records.clear();
        Ok(())
    }
    #[cfg(test)]
    pub(crate) fn fault(&self, fault: Fault) {
        self.0.lock().unwrap().fault = fault
    }
    #[cfg(test)]
    pub(crate) fn phases(&self) -> Vec<u8> {
        self.0
            .lock()
            .unwrap()
            .records
            .values()
            .map(|r| r[5])
            .collect()
    }
    #[cfg(test)]
    pub(crate) fn events(&self) -> Vec<&'static str> {
        self.0.lock().unwrap().events.clone()
    }
}
impl Boundary for Ephemeral {
    type Guard = Guard;
    fn acquire(&self, namespace: &Namespace) -> Result<Guard, Error> {
        // Namespace Debug is deliberately redacted; the facade provides a native metadata digest.
        let key = namespace.native_namespace_key();
        let mut s = self.0.lock().map_err(|_| Error::Poisoned)?;
        if s.owners.contains_key(&key) {
            return Err(Error::Busy);
        }
        let owner = thread::current().id();
        s.owners.insert(key, owner);
        Ok(Guard {
            memory: self.clone(),
            key,
            thread: owner,
            _thread_bound: PhantomData,
        })
    }
    fn abandoned(&self, _guard: &Guard) -> bool {
        false
    }
    fn read(&self, guard: &mut Guard) -> Result<Option<SecretBlob>, Error> {
        let s = self.0.lock().map_err(|_| Error::Poisoned)?;
        #[cfg(test)]
        let mut s = s;
        if thread::current().id() != guard.thread || s.owners.get(&guard.key) != Some(&guard.thread)
        {
            return Err(Error::OwnershipUncertain);
        }
        #[cfg(test)]
        {
            s.events.push("read");
            if matches!(s.fault, Fault::Read) {
                s.fault = Fault::None;
                return Err(Error::StoreUnavailable);
            }
        }
        s.records
            .get(&guard.key)
            .map(|r| SecretBlob::from_native_journal_read(r))
            .transpose()
    }
    fn write(&self, guard: &mut Guard, bytes: &[u8]) -> Result<(), Error> {
        if bytes.len() != RECORD_LEN {
            return Err(Error::Corrupt);
        }
        let mut s = self.0.lock().map_err(|_| Error::Poisoned)?;
        if thread::current().id() != guard.thread || s.owners.get(&guard.key) != Some(&guard.thread)
        {
            return Err(Error::OwnershipUncertain);
        }
        #[cfg(test)]
        {
            s.events.push("write");
            if matches!(s.fault, Fault::WriteBefore) {
                s.fault = Fault::None;
                return Err(Error::WriteUncertain);
            }
        }
        if !s.records.contains_key(&guard.key) && s.records.len() >= 8 {
            return Err(Error::StoreUnavailable);
        }
        s.records.insert(guard.key, Zeroizing::new(bytes.to_vec()));
        #[cfg(test)]
        if matches!(s.fault, Fault::WriteAfter) {
            s.fault = Fault::None;
            return Err(Error::WriteUncertain);
        }
        Ok(())
    }
}
