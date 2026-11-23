use std::{
    collections::HashMap,
    future::{poll_fn, Future},
    sync::Mutex,
    task::Poll,
    time::{Duration, Instant},
};
use tokio::sync::oneshot;

const RESERVATION_LIMIT: usize = 64;
const RESERVATION_LIFETIME: Duration = Duration::from_secs(30);
pub const CANCELLED: &str = "Test run cancelled";

struct Reservation {
    send: Option<oneshot::Sender<()>>,
    pending: Option<(Instant, oneshot::Receiver<()>)>,
}

#[derive(Default)]
pub struct TestRuns(Mutex<HashMap<String, Reservation>>);

impl TestRuns {
    pub fn prepare(&self, id: String) -> Result<(), String> {
        if id.is_empty() || id.len() > 64 {
            return Err("Invalid test run ID".into());
        }
        let mut runs = self.0.lock().map_err(|_| "Test run lock failed")?;
        let now = Instant::now();
        runs.retain(|_, run| {
            run.pending
                .as_ref()
                .is_none_or(|(expires, _)| *expires > now)
        });
        if runs.len() >= RESERVATION_LIMIT || runs.contains_key(&id) {
            return Err("Cannot reserve this test run".into());
        }
        let (send, receive) = oneshot::channel();
        runs.insert(
            id,
            Reservation {
                send: Some(send),
                pending: Some((now + RESERVATION_LIFETIME, receive)),
            },
        );
        Ok(())
    }

    pub fn start<'a>(&'a self, id: &'a str) -> Result<ActiveTestRun<'a>, String> {
        let mut runs = self.0.lock().map_err(|_| "Test run lock failed")?;
        let Some((expires, receive)) = runs.get_mut(id).and_then(|run| run.pending.take()) else {
            return Err(CANCELLED.into());
        };
        if expires <= Instant::now() {
            runs.remove(id);
            return Err(CANCELLED.into());
        }
        Ok(ActiveTestRun {
            runs: self,
            id,
            receive,
        })
    }

    pub fn cancel(&self, id: &str) -> Result<(), String> {
        let mut runs = self.0.lock().map_err(|_| "Test run lock failed")?;
        if let Some(run) = runs.get_mut(id) {
            if run.pending.is_some() {
                runs.remove(id);
            } else if let Some(send) = run.send.take() {
                let _ = send.send(());
            }
        }
        Ok(())
    }
}

pub struct ActiveTestRun<'a> {
    runs: &'a TestRuns,
    id: &'a str,
    receive: oneshot::Receiver<()>,
}

impl ActiveTestRun<'_> {
    pub fn is_cancelled(&mut self) -> bool {
        self.receive.try_recv().is_ok()
    }

    pub async fn run<T>(
        &mut self,
        work: impl Future<Output = Result<T, String>>,
    ) -> Result<T, String> {
        let mut work = std::pin::pin!(work);
        poll_fn(|cx| {
            // Check cancellation before polling the work, which can launch the runner.
            if std::pin::Pin::new(&mut self.receive).poll(cx).is_ready() {
                return Poll::Ready(Err(CANCELLED.into()));
            }
            work.as_mut().poll(cx)
        })
        .await
    }
}

impl Drop for ActiveTestRun<'_> {
    fn drop(&mut self) {
        if let Ok(mut runs) = self.runs.0.lock() {
            runs.remove(self.id);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cancellation_before_start_removes_reservation() {
        let runs = TestRuns::default();
        runs.prepare("pending".into()).unwrap();
        assert!(runs.prepare("pending".into()).is_err());
        runs.cancel("pending").unwrap();
        assert!(runs.start("pending").is_err());
        runs.prepare("pending".into()).unwrap();
    }

    #[test]
    fn expired_reservations_cannot_start() {
        let runs = TestRuns::default();
        runs.prepare("expired".into()).unwrap();
        {
            let mut state = runs.0.lock().unwrap();
            state
                .get_mut("expired")
                .unwrap()
                .pending
                .as_mut()
                .unwrap()
                .0 = Instant::now();
        }
        assert!(runs.start("expired").is_err());
        runs.prepare("expired".into()).unwrap();
    }
}
