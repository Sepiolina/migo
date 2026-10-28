use std::io;
use tokio::process::{Child, Command};

#[cfg(unix)]
pub fn configure(command: &mut Command) {
    // The runner and all of its descendants share a group owned by this invocation.
    command
        .process_group(0)
        .env("MAI_RUNNER_PROCESS_GROUP", "1");
}

#[cfg(unix)]
pub struct Guard(i32);

#[cfg(unix)]
pub fn guard(child: &Child) -> io::Result<Guard> {
    let pid = child
        .id()
        .ok_or_else(|| io::Error::other("runner has exited"))?;
    Ok(Guard(pid as i32))
}

#[cfg(unix)]
impl Drop for Guard {
    fn drop(&mut self) {
        // The group remains addressable while any descendant is alive.
        unsafe { libc::kill(-self.0, libc::SIGKILL) };
    }
}

#[cfg(windows)]
pub fn configure(_command: &mut Command) {}

#[cfg(windows)]
pub struct Guard {
    _job: std::os::windows::io::OwnedHandle,
}

#[cfg(windows)]
pub fn guard(child: &Child) -> io::Result<Guard> {
    use std::os::windows::io::{AsRawHandle, FromRawHandle};
    use windows_sys::Win32::System::JobObjects::{
        AssignProcessToJobObject, CreateJobObjectW, JobObjectExtendedLimitInformation,
        SetInformationJobObject, JOBOBJECT_EXTENDED_LIMIT_INFORMATION,
        JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
    };

    unsafe {
        let job = CreateJobObjectW(std::ptr::null(), std::ptr::null());
        if job.is_null() {
            return Err(io::Error::last_os_error());
        }
        let job = std::os::windows::io::OwnedHandle::from_raw_handle(job);
        let mut limits: JOBOBJECT_EXTENDED_LIMIT_INFORMATION = std::mem::zeroed();
        limits.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
        if SetInformationJobObject(
            job.as_raw_handle(),
            JobObjectExtendedLimitInformation,
            &limits as *const _ as *const _,
            std::mem::size_of_val(&limits) as u32,
        ) == 0
        {
            return Err(io::Error::last_os_error());
        }
        let process = child
            .raw_handle()
            .ok_or_else(|| io::Error::other("runner has exited"))?;
        if AssignProcessToJobObject(job.as_raw_handle(), process) == 0 {
            return Err(io::Error::last_os_error());
        }
        Ok(Guard { _job: job })
    }
}
